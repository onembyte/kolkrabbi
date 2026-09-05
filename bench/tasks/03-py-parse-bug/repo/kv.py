def parse_kv(line):
    """Parse "key=value" into (key, value). Values may contain "="."""
    parts = line.split("=")
    return parts[0], parts[1]
