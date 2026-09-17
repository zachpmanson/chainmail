import { afterEach } from "vitest";
import { clearToasts } from "../src/lib/toasts";

/**
 * What a write leaves to say lives in a module store rather than in the tree
 * (see lib/toasts: a mutation's callback raises it, and the shell draws it), and
 * module state outlives the test that raised it. A sentence from one test
 * standing in the corner of the next is not a fact about either of them.
 */
afterEach(() => {
  clearToasts();
});
