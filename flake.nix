{
  description = "trippy-track - self-hosted travel journal";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "trippy-track";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-BcbLHcTmaFQQIARZq8E7EMwf19GPZiYuRRmAHi1mrvc=";

          # Copy static assets and templates into the output
          postInstall = ''
            mkdir -p $out/share/trippy-track
            cp -r static templates $out/share/trippy-track/
          '';
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            sqlite
          ];
        };
      }
    );
}
