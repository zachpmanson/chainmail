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
      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          name = "chainmail";

          packages = [
            # Node 22 to match what the lockfile was resolved against. npm ships
            # with it; there is no second package manager on purpose.
            pkgs.nodejs_22
            pkgs.typescript-language-server
          ];

          shellHook = ''
            echo "chainmail  ·  node $(node --version)  npm $(npm --version)"
            echo "  npm install        once, or after package.json changes"
            echo "  npm run dev        vite, then ?spec=<path|url> or drop a file on the page"
            echo "  npm test           vitest"
            echo "  npm run typecheck  tsc --noEmit"
            echo "  npm run render -- <spec.json> -o out/page.html [--since prev.html]"
            echo "  npm run gen:types  regenerate src/lib/spec.d.ts from the schema"
          '';
        };
      });

      formatter = forAll (pkgs: pkgs.nixfmt-rfc-style);
    };
}
