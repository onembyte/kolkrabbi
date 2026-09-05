import unittest
from kv import parse_kv


class TestParseKV(unittest.TestCase):
    def test_simple(self):
        self.assertEqual(parse_kv("a=1"), ("a", "1"))

    def test_value_contains_equals(self):
        self.assertEqual(parse_kv("url=http://x/?a=b"), ("url", "http://x/?a=b"))

    def test_empty_value(self):
        self.assertEqual(parse_kv("k="), ("k", ""))


if __name__ == "__main__":
    unittest.main()
