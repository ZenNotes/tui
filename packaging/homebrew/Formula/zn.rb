class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.2/zn_0.9.2_darwin_arm64.tar.gz"
      sha256 "0eb85dc50c28855d0929b54928a86797dc1aa545901340ce7052468840d35d2c"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.2/zn_0.9.2_darwin_amd64.tar.gz"
      sha256 "d6cf4e7007dbb94ec720605dbe3369e20b006226a3ccbe22aa9a85e669493d68"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.2/zn_0.9.2_linux_arm64.tar.gz"
      sha256 "bb0c21763fe0a5a292fbe4d5a3e905aa17fea5ed7c1c65b5c9a019358a41cbf1"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.2/zn_0.9.2_linux_amd64.tar.gz"
      sha256 "57f0e231f29df65ac4c06e0052e80dbc36f1459a1844af25256cb6e869990b2c"
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
