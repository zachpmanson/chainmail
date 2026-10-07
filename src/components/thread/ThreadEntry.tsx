import type { CorpusEntry } from "../../lib/api/api";
import { MEDIA_BASE } from "../../lib/message/attachments";
import { gmailIdOf, sourceLine } from "../../lib/message/sources";
import { fetchOriginal } from "../../lib/message/original";
import { senderTitle } from "../../lib/message/who";
import { stampOf } from "../../lib/ui/stamp";
import Edits from "../specs/Edits";
import AnswerPress from "../compose/AnswerPress";
import Message from "./Message";
import ReplyLink from "./ReplyLink";
import Source from "./Source";
import { useThreadView } from "./ThreadViewContext";

/** One entry of the thread pane, wired to the thread's shared view. */
export default function ThreadEntry({ entry: e }: { entry: CorpusEntry }) {
  const lookup = useThreadView((v) => v.lookup);
  const answerExtId = useThreadView((v) => v.answerExtId);
  const aim = useThreadView((v) => v.aim);
  const flip = useThreadView((v) => v.flip);
  const pulling = useThreadView((v) => v.pulling);
  const pull = useThreadView((v) => v.pull);
  const landed = useThreadView((v) => v.landed);
  const endLanding = useThreadView((v) => v.endLanding);
  const at = stampOf(e);

  return (
    <Message
      id={lookup.anchorOf(e.extId)}
      body={e.html ?? ""}
      sender={e.author}
      senderTitle={senderTitle(e)}
      orgSlot={lookup.slot(e.org)}
      me={e.mine}
      quoted={e.quoted}
      to={e.to}
      toTitle={lookup.titleOf}
      subject={e.subject}
      source={
        <Source source={sourceLine(e, lookup.mailName)} anchorByGmail={lookup.anchorByGmail} />
      }
      reply={<ReplyLink parent={lookup.replyOf(e)} />}
      // Only for messages the mailbox holds; the server would refuse the others.
      answer={
        gmailIdOf(e) !== undefined ? (
          <AnswerPress extId={e.extId} pressed={answerExtId === e.extId} onPress={aim} />
        ) : undefined
      }
      edits={<Edits edits={lookup.editsOf(e, at)} />}
      stamp={at}
      copyJson={e}
      original={e.original ? { extId: e.extId, load: fetchOriginal } : undefined}
      fromEmail={e.fromEmail}
      // Without a person (quote-recovered entries) the switch falls back to browser storage.
      person={
        e.personId ? { id: e.personId, preferOriginal: e.preferOriginal === true } : undefined
      }
      onPreferOriginal={e.personId ? (next: boolean) => flip(e.personId!, next) : undefined}
      attachments={e.attachments}
      extId={e.extId}
      onPull={pull}
      pulling={pulling}
      mediaBase={MEDIA_BASE}
      landed={e.extId === landed}
      onLandedEnd={e.extId === landed ? endLanding : undefined}
    />
  );
}
