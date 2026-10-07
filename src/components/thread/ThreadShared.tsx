/** Thread metadata shared by list rows, previews, and the reading pane. */
export interface PreviewableThread {
  rootExtId: string;
  subject?: string;
  entries?: number;
  people?: number;
  attachments?: number;
  unread?: number;
}
