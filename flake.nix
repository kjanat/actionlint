{
  description = "GitHub Actions workflow linter";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = "1.16.0";
    in
    {
      packages = forAllSystems (pkgs: rec {
        actionlint = pkgs.callPackage ./nix/package.nix {
          src = self;
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
            go
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
