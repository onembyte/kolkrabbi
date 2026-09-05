import unittest
from money import cents_to_str


class TestCentsToStr(unittest.TestCase):
    def test_zero(self):
        self.assertEqual(cents_to_str(0), "$0.00")

    def test_simple(self):
        self.assertEqual(cents_to_str(1234), "$12.34")

    def test_sub_dollar(self):
        self.assertEqual(cents_to_str(7), "$0.07")

    def test_negative(self):
        self.assertEqual(cents_to_str(-1234), "-$12.34")


if __name__ == "__main__":
    unittest.main()
