{
  description = "GitHub Actions workflow linter";

  # Pin the Go 1.27.2 update until it reaches nixpkgs-unstable.
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
      version = "1.17.0";
    in
    {
      packages = forAllSystems (pkgs: rec {
        actionlint = pkgs.callPackage ./nix/package.nix {
          src = self;
          buildGoModule = pkgs.buildGo127Module;
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
            go_1_27
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
