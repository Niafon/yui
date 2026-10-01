/**
 * Safe Markdown for assistant replies. Raw HTML is disabled in markdown-it,
 * so model output can never inject markup; links open outside the stage.
 */
import MarkdownIt from "markdown-it";

const md = new MarkdownIt({ html: false, linkify: true, breaks: true, typographer: false });

const defaultLink = md.renderer.rules.link_open
  ?? ((tokens, index, options, _env, self) => self.renderToken(tokens, index, options));
md.renderer.rules.link_open = (tokens, index, options, env, self) => {
  const token = tokens[index]!;
  token.attrSet("target", "_blank");
  token.attrSet("rel", "noopener noreferrer");
  return defaultLink(tokens, index, options, env, self);
};
// Images from a model reply would fetch remote URLs; show them as links.
md.renderer.rules.image = (tokens, index) => {
  const token = tokens[index]!;
  const src = md.utils.escapeHtml(token.attrGet("src") ?? "");
  const alt = md.utils.escapeHtml(token.content || src);
  return `<a href="${src}" target="_blank" rel="noopener noreferrer">${alt}</a>`;
};

export function renderMarkdown(text: string): string {
  return md.render(text);
}
