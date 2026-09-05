import unittest
from slug import slugify


class TestSlugify(unittest.TestCase):
    def test_simple(self):
        self.assertEqual(slugify("Hello World"), "hello-world")

    def test_collapses_runs(self):
        self.assertEqual(slugify("a  --  b"), "a-b")

    def test_trims(self):
        self.assertEqual(slugify("  Edge  "), "edge")


if __name__ == "__main__":
    unittest.main()
