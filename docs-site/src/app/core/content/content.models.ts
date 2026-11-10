export interface PageMeta {
  slug: string;
  title: string;
  description: string;
  group: string;
  source?: string;
  api?: 'studio' | 'consumer' | 'platform';
}
export interface Article {
  html: string;
  headings: { id: string; text: string; depth: number }[];
  reference?: Omit<Article, 'reference'>;
}
