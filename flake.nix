{
  description = "GitHub Actions workflow linter";

  # Pin the required Go toolchain update until it reaches nixpkgs-unstable.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/4b4931b2f5d285574aa1fbdbbf58e6aab595d31c";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      toolchain = builtins.head (
        builtins.filter (line: nixpkgs.lib.hasPrefix "toolchain go" line) (
          nixpkgs.lib.splitString "\n" (builtins.readFile ./go.mod)
        )
      );
      goVersion = nixpkgs.lib.removePrefix "toolchain go" (nixpkgs.lib.removeSuffix "\r" toolchain);
      goParts = nixpkgs.lib.splitString "." goVersion;
      goMajor = builtins.elemAt goParts 0;
      goMinor = builtins.elemAt goParts 1;
      goFor =
        pkgs:
        let
          go = pkgs.${"go_${goMajor}_${goMinor}"};
        in
        assert go.version == goVersion;
        go;
      goBuilderFor =
        pkgs:
        assert (goFor pkgs).version == goVersion;
        pkgs.${"buildGo${goMajor}${goMinor}Module"};
      version = "1.17.0";
    in
    {
      packages = forAllSystems (pkgs: rec {
        actionlint = pkgs.callPackage ./nix/package.nix {
          src = self;
          buildGoModule = goBuilderFor pkgs;
          inherit version;
        };
        default = actionlint;
      });

      checks = forAllSystems (pkgs: {
        package = self.packages.${pkgs.stdenv.hostPlatform.system}.actionlint;
        integration = pkgs.callPackage ./nix/check.nix {
          actionlint = self.packages.${pkgs.stdenv.hostPlatform.system}.actionlint;
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            (goFor pkgs)
            git
            bash
            bash-completion
            zsh
            fish
            gnumake
            pandoc
            shellcheck
            python3Packages.pyflakes
            nixfmt
          ];
          GOTOOLCHAIN = "local";
          BASH_COMPLETION_FILE = "${pkgs.bash-completion}/share/bash-completion/bash_completion";
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
