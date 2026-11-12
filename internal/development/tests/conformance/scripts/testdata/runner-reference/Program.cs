using System;
using System.Collections.Generic;
using System.Text.Json;
using GitHub.Runner.Common;

var registered = new HashSet<string>(StringComparer.OrdinalIgnoreCase) {"notice", "error", "warning", "group", "endgroup", "debug", "echo", "add-mask", "add-matcher", "remove-matcher", "add-path", "set-env", "set-output", "save-state", "stop-commands"};
foreach (var input in new [] {"::notice:Up to date", "::error:msg", "::warning single-colon", "::group unterminated", "::notic::msg", "::notice::Up to date", "::NOTICE::ok", "  ::warning file=a,line=2::msg", "[System.IO.Path]::GetTempPath()", "example ::notic::msg", "::debug ignored-property::ok", "::add-matcher::matcher.json"}) {
    var parsed = ActionCommand.TryParseV2(input, registered, out var command);
    Console.WriteLine(JsonSerializer.Serialize(new {input, parsed, command = command?.Command, data = command?.Data}));
}

namespace GitHub.Runner.Sdk {
    public static class ArgUtil {
        public static void NotNullOrEmpty(string value, string name) {
            if (String.IsNullOrEmpty(value)) throw new ArgumentException(name);
        }
    }
}
