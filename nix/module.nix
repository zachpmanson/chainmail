# NixOS module for chainmail-server: loopback-only HTTP over the corpus.
#
# Phase 1 of issue #10: the service, loopback only, reached over an SSH
# tunnel. No new exposure, so no security work is needed — the tunnel IS the
# boundary, and chainmail-server enforces it: its checkBind refuses any
# non-loopback address before the corpus is even opened, so a misconfiguration
# here fails loudly rather than serving personal mail to the network.
#
# The corpus is transferred in by hand (`sqlite3 corpus.db "VACUUM INTO
# snapshot.db"`, then install -o chainmail), so this unit is deliberately a
# plain server — no slurper, no timers. Those are phase 2, and they belong
# behind the same docket privilege boundary the agent fleet already uses.
#
# Operator commands (ingest, embed, dedupe, twins, repair, merge, alias,
# refresh) stay CLI-only and are NOT exposed here: the HTTP surface is
# read-only by design, and a browser is the wrong place to trigger a merge
# that person_merges cannot reverse.
self: { config, lib, pkgs, ... }:

let
  cfg = config.services.chainmail;
in {
  options.services.chainmail = {
    enable = lib.mkEnableOption "chainmail server";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "The chainmail package to use (provides corpus and chainmail-server).";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8765;
      description = "Loopback port the API listens on.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "chainmail";
      description = ''
        System user running the server. Fixed rather than DynamicUser so the
        corpus can be transferred in by hand with a known owner: the
        `StateDirectory` is created and chowned to this user on start, and the
        snapshot copy step can then `install -o chainmail` into it.
      '';
    };

    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/chainmail";
      description = "Holds the corpus database (transferred in by hand).";
    };

    corpus = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/chainmail/corpus.db";
      description = "Path to the SQLite corpus the server reads.";
    };

    uploads = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = ''
        Archive upload root (Slack attachment bytes) for preview thumbnails.
        Empty embeds none: the slackdump archive is the large artefact (~1.2 GB)
        and phase 1 does not transfer it. Point this at transferred bytes when
        Slack previews are wanted on the host.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.user;
      description = "chainmail corpus server";
    };
    users.groups.${cfg.user} = { };

    systemd.services.chainmail = {
      description = "chainmail server (loopback API over the corpus)";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      serviceConfig = {
        # List form, so each argument is one word and an empty uploads root is
        # simply omitted rather than rendered as a bare `-uploads` with nothing
        # to consume.
        ExecStart = [
          "${cfg.package}/bin/chainmail-server"
          "-addr" "127.0.0.1:${toString cfg.port}"
          "-corpus" cfg.corpus
          "-uploads" cfg.uploads
        ];
        User = cfg.user;
        Group = cfg.user;
        StateDirectory = "chainmail";
        WorkingDirectory = cfg.stateDir;
        # No network namespace beyond loopback and whatever a later slurper
        # needs; ProtectSystem=strict makes the store and /etc read-only.
        ProtectSystem = "strict";
        PrivateTmp = true;
        NoNewPrivileges = true;
        # The server opens the corpus WAL-mode but this unit is read-only;
        # Restart is what keeps a transient failure from taking the tunnel down.
        Restart = "on-failure";
        RestartSec = "5s";
      };
    };
  };
}