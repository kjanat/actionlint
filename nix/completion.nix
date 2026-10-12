{
  bash-completion,
  zsh,
  fish,
  actionlint,
}:

actionlint.overrideAttrs (old: {
  pname = "actionlint-completion";
  nativeCheckInputs = old.nativeCheckInputs ++ [
    bash-completion
    zsh
    fish
  ];

  checkPhase = ''
    runHook preCheck
    export GOFLAGS=''${GOFLAGS//-trimpath/}
    export BASH_COMPLETION_FILE=${bash-completion}/share/bash-completion/bash_completion
    go test -v -run '^(${old.passthru.completionTests})$' ./internal/cli
    runHook postCheck
  '';
})
