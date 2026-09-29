class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.5.0/zn_0.5.0_darwin_arm64.tar.gz"
      sha256 "79c223408f36854574fa8a71750d8e156aa497ac775cf5450cf645a83d9fa812"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.5.0/zn_0.5.0_darwin_amd64.tar.gz"
      sha256 "2eec74e7576d53232710477e425b8c12a93ad6585bede63e806d40763fe8faa5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.5.0/zn_0.5.0_linux_arm64.tar.gz"
      sha256 "8658147d7c4a74f7865746e12b4ae3319af86a736a5127b6ddd1a235cad5e24b"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.5.0/zn_0.5.0_linux_amd64.tar.gz"
      sha256 "ebcd6edfa7767a6a6f443130e7ed34fb991b398aacefeec3c30e9ff4860e1a07"
    end
  end

  def install
    bin.install "zn"
  end

  test do
    ENV["ZENNOTES_CONFIG_DIR"] = (testpath/"config").to_s
    assert_match version.to_s, shell_output("#{bin}/zn --version")

    system bin/"zn", "init", testpath/"notes"
    (testpath/"notes/Homebrew.md").write("# Homebrew\n\nA note from the formula test.\n")
    assert_equal "# Homebrew\n\nA note from the formula test.\n",
                 shell_output("#{bin}/zn read Homebrew.md --vault #{testpath}/notes")
  end
end
