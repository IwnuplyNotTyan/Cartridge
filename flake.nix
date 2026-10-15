{
  description = "🕹️ Cartridge ~ All emulators just here";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      version = "0.1.0";
      mkCartridge = pkgs: pkgs.buildGo127Module {
        pname = "cartridge";
        inherit version;
        src = self;

        # Fill after `go.mod` changes:
        #   nix build 2>&1 | grep 'got:'
        vendorHash = "sha256-OW70+rnP6xP0nlQ/q73IcPlYFgJ/Mc32k9qKow4Dk5w=";

        ldflags = [
          "-s"
          "-w"
        ];

        meta = {
          description = "Retro games manager ✨";
          mainProgram = "cartridge";
        };
      };
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = mkCartridge pkgs;
        });
      devShells = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_27
              gotools
              golangci-lint
            ];
          };
        });
    };
}
