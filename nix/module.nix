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
# plain server — no slurper, no timers. Those are phase 2: standalone corpus
# slurp units in the machine config, running as the chainmail user against
# chainmail's own mailbox token (not behind beltino's docket boundary).
#
# Operator commands (ingest, embed, dedupe, twins, repair, merge, alias,
# refresh) stay CLI-only and are NOT exposed here: the HTTP surface is
# read-only by design, and a browser is the wrong place to trigger a merge
# that person_merges cannot reverse.
#
# One reach is exposed, opt-in: enableSlurp passes -slurp, which turns POST
# /v1/slurp into the same ingest the hourly unit runs — the browser's way to
# fetch new mail before a page refresh. Off by default, and the option alone
# grants nothing: the mailbox access is a scoped sudo the machine config
# supplies, and without it the endpoint fails to find its runner.
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

    enableSlurp = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Permit POST /v1/slurp: have the server reach the work mailbox and ingest
        it, so a saved page's refresh can build over mail that arrived since the
        last cron run. Off by default, so a host that has not granted this
        server access to the mailbox keeps the read-most posture.

        The server refuses to run a slurp unless this is on; the grant itself is
        not here. The machine config supplies the scoped sudo to the work
        mailbox's docket runner and puts that shim on this unit's PATH — this
        option only passes the flag and the shim name.
      '';
    };

    slurpBin = lib.mkOption {
      type = lib.types.str;
      default = "docket-work";
      description = "The docket shim name `corpus slurp -bin` calls, when enableSlurp is set.";
    };

    slurpTimeout = lib.mkOption {
      type = lib.types.str;
      default = "15m";
      description = "Upper bound on one /v1/slurp ingest, as a wall-clock duration string.";
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
        # A single string, not a list: NixOS renders each list element as its
        # OWN ExecStart= line (systemd supports several, running all of them),
        # and a leading '-' is systemd's "ignore failure" prefix — so a list
        # of arguments turns `-addr` into a broken ignore-failure directive.
        #
        # -uploads is appended only when non-empty: a trailing empty value is
        # dropped by systemd's arg parsing, which leaves a bare `-uploads` with
        # nothing to consume and the server refuses to start. An empty root
        # simply means the flag is absent and the server's previewer stays off
        # (dir == "" disables previews; the default path is a system user's
        # /var/empty, which holds nothing either way).
        ExecStart = "${cfg.package}/bin/chainmail-server " +
          "-addr 127.0.0.1:${toString cfg.port} " +
          "-corpus ${cfg.corpus}" +
          lib.optionalString (cfg.uploads != "") " -uploads ${cfg.uploads}" +
          lib.optionalString cfg.enableSlurp (
            " -slurp -slurp-bin ${cfg.slurpBin} -slurp-timeout ${cfg.slurpTimeout}");
        User = cfg.user;
        Group = cfg.user;
        StateDirectory = "chainmail";
        # The slurp units run as the same chainmail user (they own the work-
        # mailbox token they read), so no other principal touches the state
        # dir — 0700. StateDirectoryMode is REQUIRED, not cosmetic: systemd
        # adjusts an existing StateDirectory to this mode on every start and
        # defaults to 0755, which silently clobbers any tmpfiles mode at each
        # restart (the beltino-sharing 0770 era is over; see the machine config).
        StateDirectoryMode = "0700";
        WorkingDirectory = cfg.stateDir;
        # The server hosts the /auth/google served sign-in (chainmail#75): as
        # a system user with no home, HOME must point at the StateDirectory or
        # the OAuth flow would write the token where the slurps cannot read it
        # (the /v1/status + gmail-backend slurps read the very same store).
        Environment = "HOME=${cfg.stateDir}";
        # No network namespace beyond loopback and whatever a later slurper
        # needs; ProtectSystem=strict makes the store and /etc read-only.
        ProtectSystem = "strict";
        PrivateTmp = true;
        # setuid-denied unless the server may slurp: reaching the work mailbox is
        # a scoped sudo INTO the docket runner that holds the token, and the
        # setuid wrapper is what carries that. With slurp off the read-only
        # posture keeps NoNewPrivileges; with it on the grant is pinned to the
        # runner by the machine config, rather than to a blanket ability to
        # become root.
        NoNewPrivileges = !cfg.enableSlurp;
        # The server opens the corpus WAL-mode but this unit is read-only;
        # Restart is what keeps a transient failure from taking the tunnel down.
        Restart = "on-failure";
        RestartSec = "5s";
      };
    };
  };
}