{
  lib,
  buildGoModule,
  installShellFiles,
  makeWrapper,
  mise,
  gitMinimal,
  bash,
  pandoc,
  shellcheck,
  ruff,
  src,
  version,
}:

let
  completionTests = "TestCompletionShellBehaviour|TestCompletionScriptSyntax";
in
buildGoModule {
  pname = "actionlint";
  inherit src version;
  vendorHash = "sha256-pkXzK9G1gQTWmBhdNCE8QB51bJ1JsSHs5bo8SO5u11E=";
  subPackages = [ "cmd/actionlint" ];

  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
    "-X actionlint.kjanat.dev/v2/internal/integrations/buildinfo.ReleaseVersion=${version}"
    "-X actionlint.kjanat.dev/v2/internal/integrations/buildinfo.InstallOrigin=Nix"
  ];

  nativeBuildInputs = [
    installShellFiles
    makeWrapper
    mise
    pandoc
  ];
  nativeCheckInputs = [
    gitMinimal
    bash
    shellcheck
    ruff
  ];

  checkPhase = ''
    runHook preCheck
    export GOFLAGS=''${GOFLAGS//-trimpath/}
    go test -skip '^(${completionTests})$' ./cmd/... ./internal/... ./tools/...
    runHook postCheck
  '';

  postInstall = ''
    export MISE_CONFIG_DIR="$TMPDIR/mise-config"
    export MISE_DATA_DIR="$TMPDIR/mise-data"
    export MISE_CACHE_DIR="$TMPDIR/mise-cache"
    export MISE_STATE_DIR="$TMPDIR/mise-state"
    export MISE_ENABLE_TOOLS=""
    mkdir -p "$MISE_CONFIG_DIR" "$MISE_DATA_DIR" "$MISE_CACHE_DIR" "$MISE_STATE_DIR"
    PANDOC='pandoc --standalone --from=markdown-smart ${
      if lib.versionOlder pandoc.version "3.8" then "--no-highlight" else "--syntax-highlighting=none"
    }' mise run man:roff
    installManPage man/actionlint.1
    installShellCompletion --cmd actionlint \
      --bash <("$out/bin/actionlint" -completion bash) \
      --zsh <("$out/bin/actionlint" -completion zsh) \
      --fish <("$out/bin/actionlint" -completion fish)
    install -Dm644 actionlint.schema.json "$out/share/actionlint/actionlint.schema.json"
    cp -r schemas "$out/share/actionlint/"
    wrapProgram "$out/bin/actionlint" --prefix PATH : ${
      lib.makeBinPath [
        shellcheck
        ruff
      ]
    } --suffix PATH : ${lib.makeBinPath [ gitMinimal ]}
  '';

  passthru = { inherit completionTests; };

  meta = {
    description = "Static checker for GitHub Actions workflow files";
    homepage = "https://actionlint.kjanat.dev";
    license = lib.licenses.mit;
    mainProgram = "actionlint";
    platforms = lib.platforms.linux ++ lib.platforms.darwin;
  };
}
