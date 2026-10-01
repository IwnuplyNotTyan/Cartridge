{
  description = "🌌 Screensaver";

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
      mkMonolisa = pkgs: tags: pkgs.buildGo127Module {
        pname = "monolisa";
        inherit version;
        src = self;
        modules = ./gomod2nix.toml;
        inherit tags;

        ldflags = [
          "-s"
          "-w"
        ];

        vendorHash = "sha256-MEdSLAuG6es8mkKjsPdiNNSfA/6ojXR5NWynxkofJbQ=";

        meta = {
          description = "Screensaver without love :3";
          homepage = "https://github.com/IwnuplyNotTyan/monolisa";
          mainProgram = "monolisa";
        };
      };
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = mkMonolisa pkgs [ ];
          ssh = mkMonolisa pkgs [ "ssh" ];
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
