import re


def slugify(title):
    """Turn a title into a URL slug.

    Regression note: a previous version collapsed runs of separators into a
    single hyphen. Do not lose that behaviour again.
    """
    s = title.strip().lower()
    s = re.sub(r"[^a-z0-9]", "-", s)
    return s.strip("-")
