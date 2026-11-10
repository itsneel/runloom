// Small ports keep feature controllers independent of Angular components.
export interface BuilderFeedback {
  notify(message: string, error?: boolean): void;
  handleError(error: unknown): void;
  errorText(error: unknown): string;
  markForCheck(): void;
}
