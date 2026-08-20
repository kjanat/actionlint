#!/usr/bin/env python3

import json
import os
import subprocess
import sys
import uuid
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Literal, NotRequired, TypedDict, assert_never, cast

ACTIONLINT = "/usr/local/bin/actionlint"
SARIF_TEMPLATE = "/usr/local/share/actionlint/sarif-template.txt"
ACTIONLINT_TIMEOUT_SECONDS = 300
type FormatName = Literal[
    "github", "default", "oneline", "json", "json-lines", "markdown", "sarif"
]
type RenderableFormat = Literal[
    "github", "default", "oneline", "json", "json-lines", "markdown"
]

FORMATS: frozenset[FormatName] = frozenset(
    {"github", "default", "oneline", "json", "json-lines", "markdown", "sarif"}
)


class Problem(TypedDict):
    filepath: str
    line: int
    column: int
    end_column: int
    message: str
    kind: str
    snippet: NotRequired[str]


@dataclass(frozen=True)
class ActionInputs:
    files: list[str]
    format_name: FormatName
    ignore: list[str]
    config_file: str
    shellcheck: bool
    pyflakes: bool
    working_directory: str
    output_file: str
    fail_on_error: bool


class InputError(Exception):
    pass


def lines(value: str) -> list[str]:
    return [line for line in value.splitlines() if line]


def boolean(name: str, value: str) -> bool:
    if value == "true":
        return True
    if value == "false":
        return False
    raise InputError(f"Input '{name}' must be 'true' or 'false'")


def within_workspace(
    workspace: Path, value: str | Path, name: str, *, directory: bool
) -> Path:
    path = (workspace / value).resolve()
    try:
        _ = path.relative_to(workspace)
    except ValueError as error:
        raise InputError(
            f"Input '{name}' must stay within the repository workspace"
        ) from error
    if directory and not path.is_dir():
        raise InputError(f"Input '{name}' must identify an existing directory")
    return path


def command_escape(value: str) -> str:
    return value.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")


def property_escape(value: str) -> str:
    return command_escape(value).replace(":", "%3A").replace(",", "%2C")


def problem_header(problem: Problem) -> str:
    path = problem.get("filepath", "")
    if path.startswith("::"):
        path = f"./{path}"
    return (
        f"{path}:{problem['line']}:{problem['column']}: "
        f"{problem['message']} [{problem['kind']}]"
    )


def render_default(problems: Sequence[Problem]) -> str:
    chunks: list[str] = []
    for problem in problems:
        chunks.append(problem_header(problem) + "\n")
        snippet = problem.get("snippet", "")
        if not snippet:
            continue
        source, _, indicator = snippet.partition("\n")
        line_prefix = f"{problem['line']} | "
        indent = " " * (len(line_prefix) - 2)
        chunks.extend(
            (f"{indent}|\n", f"{line_prefix}{source}\n", f"{indent}| {indicator}\n")
        )
    return "".join(chunks)


def render_github(problems: Sequence[Problem]) -> str:
    annotations: list[str] = []
    for problem in problems:
        title = property_escape(f"actionlint ({problem['kind']})")
        path = property_escape(problem.get("filepath", ""))
        message = problem["message"]
        snippet = problem.get("snippet")
        if snippet:
            message += f"\n\n{snippet}"
        annotations.append(
            f"::error file={path},line={problem['line']},col={problem['column']},"
            + f"endColumn={problem['end_column']},title={title}::{command_escape(message)}\n"
        )
    return "".join(annotations)


def render_markdown(problems: Sequence[Problem]) -> str:
    chunks: list[str] = []
    for problem in problems:
        snippet = problem.get("snippet", "")
        indented = "\n".join(f"    {line}" for line in snippet.splitlines())
        chunks.append(
            f"### {problem['filepath']}:{problem['line']}:{problem['column']} "
            + f"({problem['kind']})\n\n{problem['message']}\n"
        )
        if indented:
            chunks.append(f"\n{indented}\n")
        chunks.append("\n")
    return "".join(chunks)


def render(
    format_name: RenderableFormat, problems: Sequence[Problem], serialized: str
) -> str:
    if format_name == "github":
        return render_github(problems)
    if format_name == "default":
        return render_default(problems)
    if format_name == "oneline":
        return "".join(problem_header(problem) + "\n" for problem in problems)
    if format_name == "json":
        return serialized
    if format_name == "json-lines":
        return "".join(
            json.dumps(problem, ensure_ascii=False, separators=(",", ":")) + "\n"
            for problem in problems
        )
    if format_name == "markdown":
        return render_markdown(problems)
    assert_never(format_name)


def append_outputs(values: Mapping[str, object]) -> None:
    output_path = os.environ.get("GITHUB_OUTPUT")
    if not output_path:
        return
    with open(output_path, "a", encoding="utf-8") as output:
        for name, value in values.items():
            value = str(value)
            delimiter = f"actionlint_{uuid.uuid4().hex}"
            while delimiter in value.splitlines():
                delimiter += "_"
            _ = output.write(f"{name}<<{delimiter}\n{value}")
            if value and not value.endswith("\n"):
                _ = output.write("\n")
            _ = output.write(f"{delimiter}\n")


def resolve_output_file(workspace: Path, relative_path: str) -> Path | None:
    if not relative_path:
        return None
    target = within_workspace(workspace, relative_path, "output-file", directory=False)
    if target.is_dir():
        raise InputError("Input 'output-file' must not identify a directory")
    return target


def write_result_file(workspace: Path, target: Path | None, content: str) -> str:
    if target is None:
        return ""
    missing_parents: list[Path] = []
    parent = target.parent
    while parent != workspace and not parent.exists():
        missing_parents.append(parent)
        parent = parent.parent
    target.parent.mkdir(parents=True, exist_ok=True)
    _ = target.write_text(content, encoding="utf-8")

    owner = workspace.stat()
    for path in reversed(missing_parents):
        os.chown(path, owner.st_uid, owner.st_gid)
    os.chown(target, owner.st_uid, owner.st_gid)
    return target.relative_to(workspace).as_posix()


def json_document(value: str) -> object:
    return cast(object, json.loads(value))


def problems_from_document(document: object) -> list[Problem]:
    if not isinstance(document, list):
        raise TypeError("JSON formatter did not return an array")
    return cast(list[Problem], document)


def sarif_problem_count(document: object) -> int:
    if not isinstance(document, dict):
        raise TypeError("SARIF formatter did not return an object")
    sarif = cast(dict[str, object], document)
    runs_value = sarif.get("runs", [])
    if not isinstance(runs_value, list):
        raise TypeError("SARIF formatter returned invalid runs")

    count = 0
    for run_value in cast(list[object], runs_value):
        if not isinstance(run_value, dict):
            raise TypeError("SARIF formatter returned an invalid run")
        run = cast(dict[str, object], run_value)
        results_value = run.get("results", [])
        if not isinstance(results_value, list):
            raise TypeError("SARIF formatter returned invalid results")
        count += len(cast(list[object], results_value))
    return count


def parse_inputs(argv: Sequence[str]) -> ActionInputs:
    if len(argv) != 10:
        raise InputError("The action received an unexpected number of inputs")

    files_input, format_input, ignore_input, config_file = argv[1:5]
    shellcheck_input, pyflakes_input, working_directory = argv[5:8]
    output_file_input, fail_on_error_input = argv[8:10]

    if format_input not in FORMATS:
        raise InputError(
            "Input 'format' must be github, default, oneline, json, json-lines, markdown, or sarif"
        )
    files = lines(files_input)
    if any(path.startswith("-") for path in files):
        raise InputError(
            "Input 'files' entries must not start with '-'; provide workflow file paths"
        )
    return ActionInputs(
        files=files,
        format_name=format_input,
        ignore=lines(ignore_input),
        config_file=config_file,
        shellcheck=boolean("shellcheck", shellcheck_input),
        pyflakes=boolean("pyflakes", pyflakes_input),
        working_directory=working_directory,
        output_file=output_file_input,
        fail_on_error=boolean("fail-on-error", fail_on_error_input),
    )


def build_command(
    inputs: ActionInputs, workspace: Path, working_dir: Path
) -> list[str]:
    command = [ACTIONLINT, "-no-color"]
    if inputs.format_name == "sarif":
        command.extend(("-format", Path(SARIF_TEMPLATE).read_text(encoding="utf-8")))
    else:
        command.extend(("-format", "{{json .}}"))
    if inputs.config_file:
        config_file = within_workspace(
            workspace,
            working_dir / inputs.config_file,
            "config-file",
            directory=False,
        )
        command.extend(("-config-file", str(config_file)))
    for pattern in inputs.ignore:
        command.extend(("-ignore", pattern))
    if not inputs.shellcheck:
        command.append("-shellcheck=")
    if not inputs.pyflakes:
        command.append("-pyflakes=")
    for path in inputs.files:
        _ = within_workspace(workspace, working_dir / path, "files", directory=False)
    command.extend(inputs.files)
    return command


def timeout_output(value: str | bytes | None) -> str:
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    return value


def run_actionlint(
    command: list[str], working_dir: Path
) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            command,
            cwd=working_dir,
            check=False,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=ACTIONLINT_TIMEOUT_SECONDS,
        )
    except subprocess.TimeoutExpired as error:
        stdout = timeout_output(error.stdout)
        stderr = timeout_output(error.stderr)
        message = f"actionlint timed out after {ACTIONLINT_TIMEOUT_SECONDS} seconds\n"
        return subprocess.CompletedProcess(command, 3, stdout, message + stderr)


def render_completed(
    completed: subprocess.CompletedProcess[str],
    command: list[str],
    format_name: FormatName,
) -> tuple[subprocess.CompletedProcess[str], int | str, str]:
    problem_count = ""
    rendered = completed.stdout
    if completed.returncode in (0, 1):
        try:
            document = json_document(completed.stdout)
            if format_name == "sarif":
                problem_count = sarif_problem_count(document)
            else:
                problems = problems_from_document(document)
                problem_count = len(problems)
                rendered = render(format_name, problems, completed.stdout)
        except (json.JSONDecodeError, TypeError, ValueError) as error:
            completed = subprocess.CompletedProcess(
                command,
                3,
                completed.stdout,
                f"could not parse actionlint output: {error}\n",
            )
            rendered = completed.stderr + completed.stdout
    elif completed.stderr:
        rendered = completed.stderr + completed.stdout
    return completed, problem_count, rendered


def emit_rendered(rendered: str, returncode: int, format_name: FormatName) -> None:
    if not rendered:
        return
    if returncode in (2, 3):
        print(f"::error title=actionlint failed::{command_escape(rendered)}")
        return
    if format_name == "github":
        _ = sys.stdout.write(rendered)
        return

    token = f"actionlint_{uuid.uuid4().hex}"
    print(f"::stop-commands::{token}")
    _ = sys.stdout.write(rendered)
    if not rendered.endswith("\n"):
        print()
    print(f"::{token}::")


def effective_exit_code(returncode: int, fail_on_error: bool) -> int:
    if returncode == 1 and not fail_on_error:
        return 0
    return returncode


def main(argv: Sequence[str]) -> int:
    inputs = parse_inputs(argv)
    workspace = Path(os.environ.get("GITHUB_WORKSPACE", os.getcwd())).resolve()
    output_file = resolve_output_file(workspace, inputs.output_file)
    working_dir = within_workspace(
        workspace,
        inputs.working_directory,
        "working-directory",
        directory=True,
    )
    command = build_command(inputs, workspace, working_dir)
    completed = run_actionlint(command, working_dir)
    completed, problem_count, rendered = render_completed(
        completed, command, inputs.format_name
    )

    result = {
        0: "success",
        1: "problems-found",
        2: "invalid-options",
        3: "failure",
    }.get(completed.returncode, "failure")
    problems_found = completed.returncode == 1
    written_file = write_result_file(workspace, output_file, rendered)

    append_outputs(
        {
            "exit-code": completed.returncode,
            "result": result,
            "problems-found": str(problems_found).lower(),
            "problem-count": problem_count,
            "output": rendered,
            "output-file": written_file,
        }
    )
    emit_rendered(rendered, completed.returncode, inputs.format_name)
    return effective_exit_code(completed.returncode, inputs.fail_on_error)


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv))
    except InputError as error:
        print(f"::error title=Invalid action input::{command_escape(str(error))}")
        sys.exit(2)
    except Exception as error:  # noqa: BLE001 -- convert unexpected action failures into annotations
        print(f"::error title=actionlint action failed::{command_escape(str(error))}")
        sys.exit(3)
