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
# refresh) stay CLI-only and are NOT exposed here: the HTTP surface is read-most
# by design, and a browser is the wrong place to trigger a merge that
# person_merges cannot reverse. The one write it can be granted is a message's
# read state, which is the mailbox's own and reversible from the same button.
#
# One reach is exposed, opt-in: enableSlurp passes -slurp, which turns POST
# /v1/slurp into the same ingest the hourly unit runs — the browser's way to
# fetch new mail before a page refresh. Off by default, and the option alone
# grants nothing: the mailbox access is a scoped sudo the machine config
# supplies, and without it the endpoint fails to find its runner.
#
# A second reach, opt-in the same way: enableMedia passes -media, which turns
# POST /v1/media/pull into one message's attachment fetch — the button under a
# message's chips, and the way a page shows a file instead of linking back to
# Gmail for it. Off by default because it spends mailbox round trips, and
# because a host that has not granted the flag should answer 403 rather than
# quietly fetching. Nothing here fetches media by itself.
#
# A third, and the first that WRITES: enableMarkRead passes -mark-read, which
# turns POST /v1/read into a label change on a chain's messages — the pane's mark
# read / mark unread button, and the reason the unread counts this server shows
# are the same ones the reader's phone shows. Off by default for a stronger
# reason than the other two: they spend mailbox round trips, this changes what is
# in the mailbox. It writes nothing on its own: every change is a request a
# person made, and the corpus's own copy of the label is reconciled from the
# mailbox by the `unread` slurp phase rather than by guessing.
#
# A fourth, and the second writer: enableMailWrite passes -mail-write, which turns
# POST /v1/mail into an archive, a delete (Gmail's own Trash) or a move for the
# chains a reader ticked — the selection bar's three mail controls. A switch of
# its own rather than folded into -mark-read: letting the read circle write is not
# the same ask as letting the page move mail out of the inbox, and every action
# here is the mailbox's own vocabulary rather than a folder model of this
# server's.
# A fifth, and the third writer — and the first that CREATES something:
# enableSendMail passes -send-mail, which turns POST /v1/send into an answer to
# one message the corpus already holds, sent from the reader's own mailbox and
# filed back into the corpus in the same request. It is the reading pane's reply
# box. Off by default, and for the strongest reason of the five: the other two
# writers change labels, which the mailbox can be reconciled with and which a
# reader can undo from the same button, while a message that has gone out cannot
# be recalled at all.
#
# What bounds it is not a permission but a shape: the endpoint answers a message
# and has no recipient field, so the address is the one that message arrived
# from. A server reachable only over a tunnel is still a server with no
# authentication, and this is the one flag here that would otherwise be an
# outbound channel to anywhere — reply-only is what keeps it answering the
# reader's own correspondence instead.
#
self: { config, lib, pkgs, ... }:

let
  cfg = config.services.chainmail;
  # The revision this build came from, for the page's deploy stamp. `self` is this
  # flake, and the machine config pins it by rev, so this is the commit that is
  # running — the same value the deployment lock holds. A dirty or tarball source
  # has no rev, and then the flag is dropped and the header shows no stamp rather
  # than a wrong commit.
  rev = self.shortRev or self.dirtyShortRev or "";
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
        last cron run. Off by default, so a host that has not given this server
        mail access keeps the read-most posture.

        The grant is the one this unit already holds: HOME points at the state
        directory, the served sign-in writes the mail token there, and the
        in-process backend reads it. Switching this on hands a page no access
        the host had not already given this user — the server refuses the
        request unless the flag is passed, and nothing else changes.
      '';
    };

    slurpTimeout = lib.mkOption {
      type = lib.types.str;
      default = "15m";
      description = "Upper bound on one /v1/slurp ingest, as a wall-clock duration string.";
    };

    enableMedia = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Permit POST /v1/media/pull: fetch one message's attachment bytes into
        the corpus, so a page can show the file instead of sending the reader to
        Gmail for it. This is what a button under a message's chips presses.
        Off by default, like enableSlurp, and for the same reason: it is the
        other surface that reaches the mailbox — once per attachment part.

        The grant is the one this unit already holds: HOME points at the state
        directory, the mail token lives there, and the pull reads it in-process
        through the same library the ingest uses. Switching this on hands a page
        no access the host had not already given this user — what changes is
        when a fetch runs, not what it may touch.
      '';
    };

    enableMarkRead = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Permit POST /v1/read: mark a chain's messages read or unread in the
        mailbox itself, and store the labels the mailbox answers with. This is
        what the pane's mark read / mark unread button presses, and it is the
        only switch here that lets the server CHANGE the mailbox rather than
        read from it — which is why it is off by default even where the other
        two are on.

        Nothing is marked by itself: a message changes only because a person
        pressed the button, and the corpus's own copy of the label is corrected
        from the mailbox by the `unread` slurp phase, not by a guess. The
        credential is the mail grant this unit already holds.
      '';
    };

    enableMailWrite = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Permit POST /v1/mail: archive, trash or move the messages of the chains
        a reader ticked, storing the labels the mailbox answers with. This is
        what the selection bar's Archive, Delete and Move controls press, and it
        is the second switch here that lets the server CHANGE the mailbox.

        A switch of its own rather than a piece of enableMarkRead, because the
        two are different asks even though both write: a host that lets the read
        circle write has not thereby asked this server to be able to move mail
        out of the inbox.

        What each action does is the mailbox's own vocabulary — archive is the
        removal of INBOX, delete is Gmail's own Trash (recoverable for thirty
        days), and a move is the target label plus the way out of the inbox — so
        nothing here invents a folder model of its own, and a name Gmail does
        not have is refused by Gmail rather than created.

        Nothing moves by itself: every change is a request a person made. The
        credential is the mail grant this unit already holds.
      '';
    };

    enableSendMail = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Permit POST /v1/send: answer one message the corpus holds, from the
        reader's own mailbox, with the message being answered quoted under their
        words. This is what the reading pane's reply box presses, and it is the
        first switch here that lets the server CREATE mail rather than change a
        message that already exists.

        Off by default for a stronger reason than the two writers beside it:
        marking read and archiving are label changes, which the corpus is
        reconciled with from the mailbox afterwards and which a reader can undo,
        while a reply that has gone out cannot be called back. Nothing is
        prepared until a person presses preview, and nothing is sent until they
        press send on the message that press showed them — the two steps are the
        endpoint's, not a client's, so no client can send without one.

        What bounds it is a shape rather than a permission: the request names
        the message being answered and has no field for a recipient, so the
        address is the one that message's own headers carried and there is
        nothing on the surface that can name a different one. That is what keeps
        a loopback-only server with no authentication from becoming an outbound
        channel to anywhere — it can answer the reader's correspondence and
        nothing else. The mail grant this unit already holds is the credential,
        and it is a send grant already.

        A reply is filed into the corpus in the same request, so the thread the
        reader is looking at shows the answer rather than waiting for the next
        slurp; if that filing fails the reply has still been sent and the
        failure is logged, because the mailbox is the source of truth.
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
          lib.optionalString (rev != "") " -rev ${rev}" +
          lib.optionalString (cfg.uploads != "") " -uploads ${cfg.uploads}" +
          lib.optionalString cfg.enableSlurp (
            " -slurp -slurp-timeout ${cfg.slurpTimeout}") +
          lib.optionalString cfg.enableMedia " -media" +
          lib.optionalString cfg.enableMarkRead " -mark-read" +
          lib.optionalString cfg.enableMailWrite " -mail-write" +
          lib.optionalString cfg.enableSendMail " -send-mail";
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
        # The server opens the corpus WAL-mode. It writes the corpus only in the
        # one way the flags above grant (a label change when -mark-read or
        # -mail-write is on, beside a rebuild); Restart is what keeps a transient
        # failure from taking the tunnel down.
        Restart = "on-failure";
        RestartSec = "5s";
      };
    };
  };
}