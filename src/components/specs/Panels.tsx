const html = (s: string) => ({ __html: s });

export interface ThreadFilter {
  /** every thread in the unfiltered trail, so excluded ones stay listed */
  chains: {
    root: string;
    subject?: string;
    opener: string;
    date: string;
    count: number;
    /** mailbox id of the thread's thread, where any entry in it names one */
    gmailId?: string;
    /** anchor of the thread's first entry, for jumping to it in the page */
    anchor: string;
  }[];
  /** thread roots currently excluded from the view */
  excluded: Set<string>;
  onToggle: (root: string) => void;
}

export { html };
