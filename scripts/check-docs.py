"""Check local documentation links and descriptive sentence lengths, without network access."""

from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
DOCUMENTS = [ROOT / "README.md", ROOT / "CONTRIBUTING.md", *sorted((ROOT / "docs").glob("*.md")),
             ROOT / "examples/reference/README.md"]


def anchors(text):
    return {re.sub(r"[^\w\- ]", "", line.lstrip("# ").lower()).replace(" ", "-")
            for line in text.splitlines() if line.startswith("#")}


def check(path):
    text = path.read_text()
    errors = []
    prose = re.sub(r"```[\s\S]*?```", "", text)
    for label, target in re.findall(r"\[([^\]]+)\]\(([^)]+)\)", prose):
        url = urlsplit(target)
        if url.scheme or url.netloc:
            continue
        destination = (path.parent / unquote(url.path)).resolve() if url.path else path
        if not destination.exists():
            errors.append(f"missing link: {target}")
        elif url.fragment and destination.suffix == ".md" and unquote(url.fragment) not in anchors(destination.read_text()):
            errors.append(f"missing anchor: {target}")
        prose = prose.replace(f"[{label}]({target})", label)
    prose = re.sub(r"`[^`]+`", "CODE", prose)
    if ";" in prose:
        errors.append("semicolon in prose; use separate sentences")
    # Tables and headings are fragments. Instructions still need a manual 20-word review.
    prose = "\n".join(line for line in prose.splitlines() if not line.startswith(("|", "#")))
    for paragraph in re.split(r"\n\s*\n", prose):
        sentences = re.split(r"(?<=[.!?])\s+", paragraph.strip())
        if len(sentences) > 6 and not re.match(r"(?:\d+\.|[-*] )", paragraph.strip()):
            errors.append("paragraph exceeds six sentences: " + paragraph[:80])
        for sentence in sentences:
            words = sentence.split()
            if len(words) > 25:
                errors.append(f"sentence has {len(words)} words: {sentence}")
    return [f"{path.relative_to(ROOT)}: {error}" for error in errors]


if __name__ == "__main__":
    errors = [error for document in DOCUMENTS for error in check(document)]
    if errors:
        print("\n".join(errors), file=sys.stderr)
        sys.exit(1)
    print("Local documentation links and description lengths passed. Manual STE review is still required.")
