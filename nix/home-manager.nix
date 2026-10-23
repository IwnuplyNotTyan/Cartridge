{ self }:
{ config, lib, pkgs, ... }:
let
  inherit (lib)
    filterAttrs
    literalExpression
    mkEnableOption
    mkIf
    mkOption
    types
    ;

  cfg = config.programs.cartridge;

  jsonFormat = pkgs.formats.json { };

  steam = filterAttrs (_: v: v != null) {
    auto_add = cfg.steam.autoAdd;
    prefix = cfg.steam.prefix;
    all_games = cfg.steam.allGames;
    category_template = cfg.steam.categoryTemplate;
    categories = cfg.steam.categories;
    exe = cfg.steam.exe;
    users = cfg.steam.users;
    icons_provider = cfg.steam.iconsProvider;
    icons_dir = cfg.steam.iconsDir;
    steamgriddb_key = cfg.steam.steamgriddbKey;
  };

  settings = filterAttrs (_: v: v != null) {
    rom_dir = cfg.romDir;
    library = cfg.library;
    lang = cfg.lang;
    show_art = cfg.showArt;
    recipes = cfg.recipes;
    patches = cfg.patches;
    mods_dir = cfg.modsDir;
    patched_dir = cfg.patchedDir;
    cheats_dir = cfg.cheatsDir;
    open_apps = cfg.openApps;
    steam = if cfg.steam == null then null else steam;
  };
in
{
  options.programs.cartridge = {
    enable = mkEnableOption "Cartridge retro game manager";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = literalExpression "cartridge.packages.\${system}.default";
      description = "The Cartridge package to install.";
    };

    romDir = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Directory containing ROMs (`rom_dir`).";
    };

    library = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Path to the library JSON file (`library`).";
    };

    lang = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "UI language code such as `en`, `ru` or `auto` (`lang`).";
    };

    showArt = mkOption {
      type = types.nullOr types.bool;
      default = null;
      description = "Show poster art previews (`show_art`).";
    };

    recipes = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Recipes file or directory (`recipes`).";
    };

    patches = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Recipe patches file (`patches`).";
    };

    modsDir = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Directory for installed mods (`mods_dir`).";
    };

    patchedDir = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Directory for hard-patched ROM instances (`patched_dir`).";
    };

    cheatsDir = mkOption {
      type = types.nullOr types.str;
      default = null; description = "Directory for cheat files (`cheats_dir`).";
    };

    openApps = mkOption {
      type = types.nullOr types.bool;
      default = null;
      description = "Launch through `open -a` on macOS (`open_apps`).";
    };

    steam = mkOption {
      type = types.nullOr (types.submodule {
        options = {
          autoAdd = mkOption {
            type = types.bool;
            default = false;
            description = "Automatically add games as Steam shortcuts (`auto_add`).";
          };

          prefix = mkOption {
            type = types.str;
            default = "Cartridge";
            description = "Root shortcut prefix; empty disables it (`prefix`).";
          };

          allGames = mkOption {
            type = types.bool;
            default = false;
            description = "Add a shared category for all games (`all_games`).";
          };

          categoryTemplate = mkOption {
            type = types.str;
            default = "{prefix} - {name}";
            description = "Per-console category template (`category_template`).";
          };

          categories = mkOption {
            type = types.attrsOf types.str;
            default = { };
            description = "Per-console category names (`categories`).";
          };

          exe = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "Custom launch executable (`exe`).";
          };

          users = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Steam user IDs to write shortcuts for (`users`).";
          };

          iconsProvider = mkOption {
            type = types.enum [ "auto" "libretro" "steamgriddb" "off" ];
            default = "auto";
            description = "Artwork provider (`icons_provider`).";
          };

          iconsDir = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "Artwork cache directory (`icons_dir`).";
          };

          steamgriddbKey = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "SteamGridDB API key (`steamgriddb_key`).";
          };
        };
      });
      default = null;
      description = "Steam non-Steam shortcuts integration (`steam`).";
    };
  };

  config = mkIf cfg.enable {
    home.packages = [ cfg.package ];

    xdg.configFile."cartridge/config.json" = mkIf (settings != { }) {
      source = jsonFormat.generate "cartridge-config.json" settings;
      force = true;
    };
  };
}
