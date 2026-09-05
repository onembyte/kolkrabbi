import sqlite3

SCHEMA = """
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, cents INTEGER);
"""


def connect():
    db = sqlite3.connect(":memory:")
    db.executescript(SCHEMA)
    return db


def totals_by_user(db):
    """Return [(name, total_cents)] for every user, including those with no orders."""
    rows = db.execute(
        """
        SELECT users.name, COALESCE(SUM(orders.cents), 0)
        FROM users
        JOIN orders ON orders.user_id = users.id
        GROUP BY users.id
        ORDER BY users.name
        """
    ).fetchall()
    return [(r[0], r[1]) for r in rows]
