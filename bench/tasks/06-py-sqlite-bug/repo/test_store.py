import unittest
from store import connect, totals_by_user


class TestTotals(unittest.TestCase):
    def setUp(self):
        self.db = connect()
        self.db.executemany("INSERT INTO users VALUES (?, ?)", [(1, "ada"), (2, "bob")])
        self.db.executemany(
            "INSERT INTO orders VALUES (?, ?, ?)", [(1, 1, 500), (2, 1, 250)]
        )

    def test_includes_user_with_orders(self):
        self.assertIn(("ada", 750), totals_by_user(self.db))

    def test_includes_user_without_orders(self):
        self.assertIn(("bob", 0), totals_by_user(self.db))


if __name__ == "__main__":
    unittest.main()
