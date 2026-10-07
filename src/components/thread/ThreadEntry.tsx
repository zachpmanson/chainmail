import type { CorpusEntry } from "../../lib/api/api";
import { MEDIA_BASE } from "../../lib/message/attachments";
import { gmailIdOf, sourceLine } from "../../lib/message/sources";
import { fetchOriginal } from "../../lib/message/original";
import { emailFromCorpus } from "../../lib/message/email";
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
  const email = emailFromCorpus(e);

  return (
    <Message
      email={email}
      place={{
        id: lookup.anchorOf(e.extId),
        landed: e.extId === landed,
        onLandedEnd: e.extId === landed ? endLanding : undefined,
      }}
      look={{ orgSlot: lookup.slot(e.org), toTitle: lookup.titleOf }}
      media={{ onPull: pull, pulling, mediaBase: MEDIA_BASE }}
      reading={
        e.original
          ? {
              original: { extId: e.extId, load: fetchOriginal },
              // Without a person (quote-recovered entries) the switch falls back to browser storage.
              person: e.personId
                ? { id: e.personId, preferOriginal: e.preferOriginal === true }
                : undefined,
              onPreferOriginal: e.personId ? (next: boolean) => flip(e.personId!, next) : undefined,
            }
          : undefined
      }
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
      edits={<Edits edits={lookup.editsOf(e, email.stamp)} />}
      copyJson={e}
    />
  );
}
