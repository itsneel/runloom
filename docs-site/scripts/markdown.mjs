import { Marked } from 'marked';
import sanitizeHtml from 'sanitize-html';
import GithubSlugger from 'github-slugger';

export const plainText = (html) =>
  sanitizeHtml(html, { allowedTags: [], allowedAttributes: {} })
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"');
export const escape = (text) =>
  String(text)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');

export function renderMarkdown(body, resolveLink = (href) => href, headingPrefix = '') {
  const slugger = new GithubSlugger();
  const headings = [];
  const links = [];
  const parser = new Marked({
    renderer: {
      html() {
        return '';
      }, // Source HTML is not executable documentation.
      heading({ tokens, depth }) {
        const html = this.parser.parseInline(tokens);
        const text = plainText(html);
        const id = headingPrefix + slugger.slug(text);
        if (depth === 1) return ''; // The Angular article header owns the single H1.
        if (depth <= 3) headings.push({ id, text, depth });
        return `<h${depth} id="${escape(id)}">${html}</h${depth}>`;
      },
      link({ href, tokens }) {
        const label = this.parser.parseInline(tokens);
        const target = resolveLink(href);
        if (!target || !/^(?:https?:\/\/|\/[^/]|#)/.test(target)) return label;
        links.push(target);
        return `<a href="${escape(target)}"${target.startsWith('http') ? ' rel="noopener noreferrer"' : ''}>${label}</a>`;
      },
      image({ text }) {
        return escape(text);
      },
      code({ text, lang }) {
        const label = lang?.split(/\s/)[0] || 'Code';
        return `<div class="code-block"><div class="code-heading"><span>${escape(label)}</span><button type="button" data-copy="true" aria-label="Copy code">Copy</button></div><pre tabindex="0" role="region" aria-label="Code example"><code>${escape(text)}</code></pre></div>`;
      },
    },
  });
  const html = sanitizeHtml(parser.parse(body), {
    allowedTags: [...sanitizeHtml.defaults.allowedTags, 'button'],
    allowedAttributes: {
      ...sanitizeHtml.defaults.allowedAttributes,
      '*': ['id', 'class'],
      button: ['type', 'data-copy', 'aria-label'],
      pre: ['tabindex', 'role', 'aria-label'],
      table: ['tabindex', 'aria-label'],
    },
    transformTags: {
      table: sanitizeHtml.simpleTransform('table', {
        tabindex: '0',
        'aria-label': 'Reference table',
      }),
    },
    allowedSchemes: ['http', 'https'],
    allowProtocolRelative: false,
  });
  return { html, headings, links, text: plainText(html) };
}
