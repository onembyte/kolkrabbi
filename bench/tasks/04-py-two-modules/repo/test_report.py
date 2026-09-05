import unittest
from report import render_line


class TestRenderLine(unittest.TestCase):
    def test_uses_currency_format(self):
        self.assertEqual(render_line("rent", 120000), "rent: $1200.00")


if __name__ == "__main__":
    unittest.main()
