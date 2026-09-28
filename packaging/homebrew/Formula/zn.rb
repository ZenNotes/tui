class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.1/zn_0.4.1_darwin_arm64.tar.gz"
      sha256 "3c83f847268c26279121537c807785dfab0403fa0cae248f149d09a11a2e2eda"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.1/zn_0.4.1_darwin_amd64.tar.gz"
      sha256 "d541dcc3fbfd15d1f702a71ff4d629d1a1907a21d055c23df27934925e9f4557"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.1/zn_0.4.1_linux_arm64.tar.gz"
      sha256 "55d6999163b66fc0e29a3a35a449f37175c448bb7e6e22072ee1c970769fd739"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.1/zn_0.4.1_linux_amd64.tar.gz"
      sha256 "42e5e64edde08f3145883ffcfeaf197d6290d966074ce0e28f054d254bb806c1"
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
