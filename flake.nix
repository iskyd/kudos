{
  description = "KudOS development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      devShells = forAllSystems (pkgs: {
        # sqlc, oapi-codegen, goose, river and mockgen are pinned as Go tools in go.mod (`go tool ...`), not here.
        # Docker and Docker Compose come from the host: the daemon can't run from a dev shell.
        default = pkgs.mkShell {
          packages = with pkgs; [
            just
            pre-commit
            python3Packages.pre-commit-hooks # hook executables, so no hook env is downloaded
            gitleaks
            go
            gopls
            golangci-lint
            air # live reload for `just dev`
            nodejs_22
            postgresql # psql client for poking at the dev TimescaleDB
            playwright-driver.browsers
          ];

          # shortcut: Nix browsers only work with the npm @playwright/test version matching nixpkgs' playwright-driver; pin it to that version in web/package.json.
          PLAYWRIGHT_BROWSERS_PATH = "${pkgs.playwright-driver.browsers}";
          PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS = "true";
          PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";

          # Re-installed on every entry: the hook script pins a Nix store path that changes on updates.
          shellHook = ''
            if [ -d .git ]; then pre-commit install >/dev/null; fi
          '';
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
