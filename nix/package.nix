{
  lib,
  buildGoModule,
  installShellFiles,
  makeWrapper,
  git,
  bash,
  bash-completion,
  zsh,
  fish,
  pandoc,
  shellcheck,
  python3Packages,
  src,
  version,
}:

buildGoModule {
  pname = "actionlint";
  inherit src version;
  vendorHash = "sha256-OLlj12BiG9bSmniz91NI3LwkKWP7unJZQsYlQBHtbwQ=";
  subPackages = [ "cmd/actionlint" ];

  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
    "-X actionlint.kjanat.dev.version=${version}"
    "-X actionlint.kjanat.dev.installedFrom=Nix"
  ];

  nativeBuildInputs = [
    installShellFiles
    makeWrapper
    pandoc
  ];
  nativeCheckInputs = [
    git
    bash
    bash-completion
    zsh
    fish
    shellcheck
    python3Packages.pyflakes
  ];

  checkPhase = ''
    runHook preCheck
    export GOFLAGS=''${GOFLAGS//-trimpath/}
    export BASH_COMPLETION_FILE=${bash-completion}/share/bash-completion/bash_completion
    go test ./...
    runHook postCheck
  '';

  postInstall = ''
    make man/actionlint.1 PANDOC='pandoc --standalone --from=markdown-smart ${
      if lib.versionOlder pandoc.version "3.8" then "--no-highlight" else "--syntax-highlighting=none"
    }'
    installManPage man/actionlint.1
    installShellCompletion --cmd actionlint \
      --bash <("$out/bin/actionlint" -completion bash) \
      --zsh <("$out/bin/actionlint" -completion zsh) \
      --fish <("$out/bin/actionlint" -completion fish)
    install -Dm644 actionlint.schema.json "$out/share/actionlint/actionlint.schema.json"
    wrapProgram "$out/bin/actionlint" --prefix PATH : ${
      lib.makeBinPath [
        shellcheck
        python3Packages.pyflakes
      ]
    }
  '';

  meta = {
    description = "Static checker for GitHub Actions workflow files";
    homepage = "https://actionlint.kjanat.dev";
    license = lib.licenses.mit;
    mainProgram = "actionlint";
    platforms = lib.platforms.linux ++ lib.platforms.darwin;
  };
}
