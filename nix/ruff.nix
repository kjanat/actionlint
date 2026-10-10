{
  lib,
  stdenv,
  fetchurl,
  autoPatchelfHook,
}:

let
  release = builtins.fromJSON (builtins.readFile ../packages/github-action/tools/ruff.json);
  targets = {
    x86_64-linux = "x86_64-unknown-linux-gnu";
    aarch64-linux = "aarch64-unknown-linux-gnu";
    aarch64-darwin = "aarch64-apple-darwin";
  };
  target = targets.${stdenv.hostPlatform.system};
  asset = lib.findFirst (asset: asset.name == "ruff-${target}.tar.gz") (
    throw "Missing Ruff release asset for ${stdenv.hostPlatform.system}"
  ) release.assets;
in
stdenv.mkDerivation {
  pname = "ruff";
  version = release.tagName;
  src = fetchurl {
    inherit (asset) url;
    sha256 = lib.removePrefix "sha256:" asset.digest;
  };

  nativeBuildInputs = lib.optionals stdenv.hostPlatform.isLinux [ autoPatchelfHook ];
  buildInputs = lib.optionals stdenv.hostPlatform.isLinux [ stdenv.cc.cc.lib ];
  dontConfigure = true;
  dontBuild = true;
  dontStrip = true;

  installPhase = ''
    runHook preInstall
    install -Dm755 ruff "$out/bin/ruff"
    runHook postInstall
  '';

  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    test "$("$out/bin/ruff" --version)" = ${lib.escapeShellArg "ruff ${release.tagName}"}
    runHook postInstallCheck
  '';

  meta = {
    description = "Extremely fast Python linter and code formatter";
    homepage = "https://github.com/astral-sh/ruff";
    license = lib.licenses.mit;
    mainProgram = "ruff";
    platforms = builtins.attrNames targets;
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
  };
}
