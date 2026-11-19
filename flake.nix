{
  description = "GitHub Actions workflow linter";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  # Go 1.27.4 as Hydra built it on staging-next, so Linux fetches it from cache.nixos.org.
  inputs.nixpkgs-go = {
    url = "github:NixOS/nixpkgs/294345d17e2f91dcfaa12cd7025dbd459a2eaea2";
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
          # Hydra's staging-next jobset builds Linux only; Darwin builds the same recipe locally.
          go =
            if pkgs.stdenv.hostPlatform.isLinux then
              (import nixpkgs-go { inherit (pkgs.stdenv.hostPlatform) system; })."go_${goMajor}_${goMinor}"
            else
              pkgs.callPackage (nixpkgs-go + "/pkgs/development/compilers/go/${goMajor}.${goMinor}.nix") { };
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
