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
      mkCartridge = pkgs: pkgs.buildGo127Module {
        pname = "cartridge";
        inherit version;
        src = self;

        # Fill after `go.mod` changes:
        #   nix build 2>&1 | grep 'got:'
        vendorHash = "sha256-g9mOf8ncNAszYD8EMeTyQfsKP9BE4HuYLK871ikxFk4=";

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
            ];
          };
        });
    };
}
