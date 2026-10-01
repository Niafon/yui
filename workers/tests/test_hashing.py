"""The hash embedding must stay byte-for-byte compatible with the Go core."""

import sys, pathlib
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

import unittest

from yui_worker.hashing import cosine, fnv1a32, hash_vector, tokenize


class TestFNV(unittest.TestCase):
    def test_known_vectors(self):
        # Reference values for FNV-1a 32 bit; Go's hash/fnv produces the same.
        self.assertEqual(fnv1a32(""), 0x811C9DC5)
        self.assertEqual(fnv1a32("a"), 0xE40C292C)
        self.assertEqual(fnv1a32("foobar"), 0xBF9CF968)

    def test_utf8_is_hashed_by_bytes(self):
        self.assertEqual(fnv1a32("чай"), fnv1a32("чай"))
        self.assertNotEqual(fnv1a32("чай"), fnv1a32("кофе"))


class TestTokenize(unittest.TestCase):
    def test_drops_punctuation_and_single_chars(self):
        self.assertEqual(tokenize("Привет, мир! я тут"), ["привет", "мир", "тут"])

    def test_mixed_alphabets(self):
        self.assertEqual(tokenize("Yui и llama.cpp"), ["yui", "llama", "cpp"])


class TestHashVector(unittest.TestCase):
    def test_normalised(self):
        vec = hash_vector("любимый напиток чай")
        norm = sum(v * v for v in vec) ** 0.5
        self.assertAlmostEqual(norm, 1.0, places=6)

    def test_similar_texts_are_closer(self):
        query = hash_vector("какой напиток я люблю")
        related = hash_vector("любимый напиток пользователя чай")
        unrelated = hash_vector("завтра встреча с командой в офисе")
        self.assertGreater(cosine(query, related), cosine(query, unrelated))

    def test_empty_text_gives_zero_vector(self):
        self.assertEqual(sum(abs(v) for v in hash_vector("")), 0.0)


if __name__ == "__main__":
    unittest.main()
