{
  description = "trippy-track - self-hosted travel journal";

  inputs.nixpkgs.url = "nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";
      version = builtins.substring 0 8 lastModifiedDate;
      supportedSystems = [
        "x86_64-linux"
        "x86_64-darwin"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
        in
        rec {
          trippy-track = pkgs.buildGoModule {
            pname = "trippy-track";
            inherit version;
            src = ./.;

            vendorHash = "sha256-UnLMf1SD8EnwF+qaoCgFTdd5/fAeSDAFFoCg5Gz9Db4=";

            postInstall = ''
              mkdir -p $out/share/trippy-track
              cp -r static templates $out/share/trippy-track/
            '';
          };
          default = trippy-track;
        }
      );

      devShells = forAllSystems (
        system:
        with nixpkgsFor.${system};
        {
          default = mkShell {
            buildInputs = [
              go
              gopls
              gotools
              sqlite
            ];
          };
        }
      );

      nixosModules.default = { config, lib, pkgs, ... }:
        let
          cfg = config.services.trippy-track;
        in
        {
          options.services.trippy-track = {
            enable = lib.mkEnableOption "trippy-track travel journal";

            port = lib.mkOption {
              type = lib.types.port;
              default = 8080;
              description = "Port to listen on";
            };

            dataDir = lib.mkOption {
              type = lib.types.path;
              default = "/var/lib/trippy-track";
              description = "Directory for database and uploads";
            };

            environmentFile = lib.mkOption {
              type = lib.types.nullOr lib.types.path;
              default = null;
              description = "Environment file with OIDC secrets (OIDC_ISSUER_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, OIDC_REDIRECT_URL)";
            };

            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.system}.trippy-track;
              description = "trippy-track package to use";
            };
          };

          config = lib.mkIf cfg.enable {
            systemd.services.trippy-track = {
              description = "Trippy Track - self-hosted travel journal";
              wantedBy = [ "multi-user.target" ];
              after = [ "network-online.target" ];
              wants = [ "network-online.target" ];

              environment = {
                PORT = toString cfg.port;
                DATABASE_URL = "${cfg.dataDir}/trippy-track.db";
                UPLOADS_DIR = "${cfg.dataDir}/uploads";
              };

              serviceConfig = {
                Type = "simple";
                DynamicUser = true;
                StateDirectory = "trippy-track";
                WorkingDirectory = "${cfg.package}/share/trippy-track";
                ExecStart = "${cfg.package}/bin/trippy-track";
                Restart = "on-failure";
                RestartSec = 5;

                # Hardening
                NoNewPrivileges = true;
                ProtectSystem = "strict";
                ProtectHome = true;
                PrivateTmp = true;
                ReadWritePaths = [ cfg.dataDir ];
              } // lib.optionalAttrs (cfg.environmentFile != null) {
                EnvironmentFile = cfg.environmentFile;
              };
            };
          };
        };
    };
}
