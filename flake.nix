{
  description = "GitHub Actions workflow linter";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  # Use the Go 1.27.4 recipe with the cached dependencies from nixpkgs-unstable.
  inputs.nixpkgs-go = {
    url = "github:NixOS/nixpkgs/6f0c298a612204a69074aee91d87dc397d767d3e";
    flake = false;
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-go,
    }:
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
          go = pkgs.callPackage (nixpkgs-go + "/pkgs/development/compilers/go/${goMajor}.${goMinor}.nix") { };
        in
        assert go.version == goVersion;
        go;
      goBuilderFor = pkgs: pkgs.buildGoModule.override { go = goFor pkgs; };
      version = nixpkgs.lib.trim (builtins.readFile ./VERSION);
    in
    {
      packages = forAllSystems (pkgs: rec {
        actionlint = pkgs.callPackage ./distribution/nix/package.nix {
          src = self;
          buildGoModule = goBuilderFor pkgs;
          inherit version;
        };
        default = actionlint;
      });

      checks = forAllSystems (pkgs: {
        package = self.packages.${pkgs.stdenv.hostPlatform.system}.actionlint;
        integration = pkgs.callPackage ./distribution/nix/check.nix {
          actionlint = self.packages.${pkgs.stdenv.hostPlatform.system}.actionlint;
        };
        completion = pkgs.callPackage ./distribution/nix/completion.nix {
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
            mise
            zsh
            fish
            pandoc
            nixfmt
          ];
          GOTOOLCHAIN = "local";
          BASH_COMPLETION_FILE = "${pkgs.bash-completion}/share/bash-completion/bash_completion";
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
