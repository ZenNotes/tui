import copy
from pathlib import Path
import unittest

from update_formula import render_formula


class UpdateFormulaTests(unittest.TestCase):
    def setUp(self):
        self.formula = Path(__file__).with_name("Formula").joinpath("zn.rb").read_text()
        self.release = {
            "tag_name": "v1.2.3",
            "draft": False,
            "prerelease": False,
            "assets": [
                {"name": f"zn_1.2.3_{target}.tar.gz", "digest": "sha256:" + str(i) * 64}
                for i, target in enumerate(
                    ("darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64"), 1
                )
            ],
        }

    def test_updates_version_and_all_platform_checksums(self):
        result = render_formula(self.formula, "1.2.3", self.release)
        for asset in self.release["assets"]:
            digest = asset["digest"].removeprefix("sha256:")
            self.assertIn(f'/v1.2.3/{asset["name"]}"\n      sha256 "{digest}"', result)

    def test_rejects_incomplete_or_invalid_release_metadata(self):
        cases = []
        for field, value in (("draft", True), ("prerelease", True), ("tag_name", "v1.2.4")):
            release = copy.deepcopy(self.release)
            release[field] = value
            cases.append(release)
        for digest in (None, "", "sha256:invalid"):
            release = copy.deepcopy(self.release)
            release["assets"][-1]["digest"] = digest
            cases.append(release)
        release = copy.deepcopy(self.release)
        release["assets"].pop()
        cases.append(release)
        for release in cases:
            with self.subTest(release=release), self.assertRaises(ValueError):
                render_formula(self.formula, "1.2.3", release)

    def test_rejects_formula_without_all_platforms(self):
        formula = self.formula.replace("linux_amd64.tar.gz", "linux_386.tar.gz")
        with self.assertRaises(ValueError):
            render_formula(formula, "1.2.3", self.release)


if __name__ == "__main__":
    unittest.main()
