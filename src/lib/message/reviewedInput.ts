/** A review belongs to one immutable request and one uninterrupted editor generation. */
export type ReviewedInput<T> = { readonly input: T; readonly generation: number };

export default class ReviewInput<T> {
  private key = "";
  private generation = 0;

  update(input: T) {
    const key = JSON.stringify(input);
    if (key !== this.key) {
      this.key = key;
      this.invalidate();
    }
  }

  invalidate() {
    this.generation++;
  }

  capture(): ReviewedInput<T> {
    // A newer preview request also supersedes any older in-flight preview.
    this.invalidate();
    return { input: JSON.parse(this.key) as T, generation: this.generation };
  }

  isCurrent(reviewed: ReviewedInput<T>) {
    return reviewed.generation === this.generation;
  }
}
