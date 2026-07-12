import { describe, expect, it } from "vitest";
import { safeArticleHTML } from "./sanitize";

describe("safeArticleHTML", () => {
  it("removes executable markup and unsafe attributes", () => {
    const result = safeArticleHTML('<script>alert(1)</script><p onclick="alert(2)">正文</p><a href="https://example.com">来源</a>', "");
    expect(result).not.toContain("script");
    expect(result).not.toContain("onclick");
    expect(result).toContain("正文");
    expect(result).toContain('target="_blank"');
    expect(result).toContain("noopener");
  });

  it("converts plain text paragraphs without interpreting HTML", () => {
    const result = safeArticleHTML("第一段\n\n第二段 <unsafe>", "");
    expect(result).toBe("<p>第一段</p><p>第二段 &lt;unsafe&gt;</p>");
  });

  it("uses fallback text for empty content", () => {
    expect(safeArticleHTML("", "备用摘要")).toBe("<p>备用摘要</p>");
  });
});
