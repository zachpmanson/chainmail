{
  description = "chainmail — read an email trail as one chronological transcript";

  # Pinned to the same channel as the machine config in ~/nix, so the toolchain
  # here matches the system's rather than drifting a release ahead.
  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-26.05";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      # One package, both binaries. `make install` builds them as a pair and the
      # server is useless without a corpus for it to read, so splitting them
      # would buy a machine the CLI alone at the cost of two derivations that
      # share a source tree, a vendorHash and a test suite. Split it only if a
      # host ever wants `corpus` without the service.
      #
      # The web client is embedded INTO the server: vite build → cmd/server/dist,
      # go:embed → one binary serving API + UI. Deps are fetched through
      # buildNpmPackage's npmDepsHash — a fixed-output derivation nix DOES give
      # network access (a plain `npm ci` in preBuild is sandboxed with no network
      # and hangs forever). The hash makes the fetch cached and reproducible:
      # slow once, instant every later build and every deploy.
      packages = forAll (pkgs: rec {
        default = chainmail;

        # The web client build. buildNpmPackage runs npm ci (offline, from the
        # locked hash) then `npm run build`, which vite sends to cmd/server/dist
        # inside the copied source. We then copy that dist into the Go build's
        # source in preBuild so cmd/server's go:embed resolves.
        webClient = pkgs.buildNpmPackage {
          pname = "chainmail-web";
          version = "0.1.0";
          src = self;
          nodejs = pkgs.nodejs_22;
          npmDepsHash = "sha256-jteAHYoIjYgk4TzZaglMhOYbyXoJHqi6Wgp6O+GGGA4=";
          # The default npmInstallHook re-installs from package.json, which
          # drops the vite-built cmd/server/dist (untracked, not in `files`).
          # The only artifact this package must ship IS that dist, so the
          # install phase is just: copy it out.
          dontNpmInstall = true;
          installPhase = ''
            mkdir -p $out/dist
            cp -r cmd/server/dist/. $out/dist/
          '';
        };

        chainmail = pkgs.buildGoModule {
          pname = "chainmail";
          version = "0.1.0";
          src = self;
          vendorHash = "sha256-tE5twZddLbKWD6TyN1y+c8KkKh1TvLbKb2VViEIPHXQ=";

          # copy the vite-built client into the Go source so go:embed finds it.
          preBuild = ''
            mkdir -p cmd/server/dist
            cp -r ${webClient}/dist/. cmd/server/dist/
          '';

          # The suite reads committed fixtures and an embedded tzdata, never the
          # network, docket or $HOME, so it is safe to gate the install on it.
          doCheck = true;

          # `cmd/server` would install as plain `server`, which is far too
          # generic a name to put on a user's PATH. This is the name the Makefile
          # builds and the README tells people to run.
          postInstall = ''
            mv "$out/bin/server" "$out/bin/chainmail-server"
          '';

          meta = {
            description = "read an email trail as one chronological transcript";
            mainProgram = "corpus";
          };
        };
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          name = "chainmail";

          packages = [
            # Node 22 to match what the lockfile was resolved against. npm ships
            # with it; there is no second package manager on purpose.
            pkgs.nodejs_22
            pkgs.typescript-language-server
            # the backend (corpus, ingest, later the server) is Go
            pkgs.go
            pkgs.gopls
            pkgs.sqlite            # for poking at a corpus by hand
          ];

          shellHook = ''
            echo "chainmail  ·  node $(node --version)  npm $(npm --version)"
            echo "  npm install        once, or after package.json changes"
            echo "  npm run dev        vite, then ?spec=<path|url> or drop a file on the page"
            echo "  npm test           vitest"
            echo "  npm run typecheck  tsc --noEmit"
            echo "  npm run render -- <spec.json> -o out/page.html [--since prev.html]"
            echo "  npm run gen:types  regenerate src/lib/spec.d.ts from the schema"
            echo "  go test ./...      corpus + ingest"
            echo "  go run ./cmd/corpus init | stats | ingest mail -q <query>"
          '';
        };
      });

      formatter = forAll (pkgs: pkgs.nixfmt-rfc-style);

      # The NixOS service module (nix/module.nix): runs chainmail-server on
      # loopback. Imported by the machine config that deploys a corpus; the
      # module owns the unit, the config owns the corpus transfer.
      nixosModules.default = import ./nix/module.nix self;
    };
}
