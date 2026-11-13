{
  lib,
  buildGoModule,
  installShellFiles,
  makeWrapper,
  gitMinimal,
  bash,
  pandoc,
  src,
  version,
}:

let
  completionTests = "TestCompletionShellBehaviour|TestCompletionScriptSyntax";
in
buildGoModule {
  pname = "actionlint";
  inherit src version;
  vendorHash = "sha256-PSSL3h8aYd+OanNP62MUk+j+z+CsOgFKJQh8p0MUSjM=";
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
    pandoc
  ];
  nativeCheckInputs = [
    gitMinimal
    bash
  ];

  checkPhase = ''
    runHook preCheck
    export GOFLAGS=''${GOFLAGS//-trimpath/}
    go test -skip '^(${completionTests})$' ./cmd/... ./internal/...
    runHook postCheck
  '';

  postInstall = ''
    pandoc --standalone --from=markdown-smart \
      --to=man --metadata=title:ACTIONLINT --lua-filter=man/release-version.lua --lua-filter=man/inline-code-bold.lua \
      ${
        if lib.versionOlder pandoc.version "3.8" then "--no-highlight" else "--syntax-highlighting=none"
      } --output=man/actionlint.1 man/actionlint.1.md
    installManPage man/actionlint.1
    installShellCompletion --cmd actionlint \
      --bash <("$out/bin/actionlint" -completion bash) \
      --zsh <("$out/bin/actionlint" -completion zsh) \
      --fish <("$out/bin/actionlint" -completion fish)
    install -Dm644 actionlint.schema.json "$out/share/actionlint/actionlint.schema.json"
    cp -r schemas "$out/share/actionlint/"
    wrapProgram "$out/bin/actionlint" --suffix PATH : ${lib.makeBinPath [ gitMinimal ]}
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
