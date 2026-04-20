{
  description = "trip-track - self-hosted travel journal";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "trip-track";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-BcbLHcTmaFQQIARZq8E7EMwf19GPZiYuRRmAHi1mrvc=";

          # Copy static assets and templates into the output
          postInstall = ''
            mkdir -p $out/share/trip-track
            cp -r static templates $out/share/trip-track/
          '';
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            sqlite
          ];
        };
      });
}
