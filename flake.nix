{
  description = "🕹️ Cartridge ~ All emulators just here";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    nixpkgs-darwinx86.url = "github:NixOS/nixpkgs/nixpkgs-26.05-darwin";
  };

  outputs = { self, nixpkgs, nixpkgs-darwinx86, ... }:
    let
      systems = [
        "x86_64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor = system:
        if system == "x86_64-darwin"
        then nixpkgs-darwinx86.legacyPackages.${system}
        else nixpkgs.legacyPackages.${system};
      version = "0.1.0";
      hmModule = import ./nix/home-manager.nix { inherit self; };
      mkCartridge = pkgs: pkgs.buildGo127Module {
        pname = "cartridge";
        inherit version;
        src = self;

        # Updated automatically by nix/update-vendor-hash.sh.
        # Run it locally after changing go.mod/go.sum:
        #   ./nix/update-vendor-hash.sh
        vendorHash = "sha256-KyJVujaWkeYOOHBhcS2G9k0E1tUVXRiDfZNWR99y/r8=";

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
          pkgs = pkgsFor system;
        in
        {
          default = mkCartridge pkgs;
        });
      homeModules.default = hmModule;
      devShells = forAllSystems (system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_27
              gotools
              golangci-lint
              govulncheck
              gosec
              actionlint
              shellcheck
            ];
          };
        });
    };
}
