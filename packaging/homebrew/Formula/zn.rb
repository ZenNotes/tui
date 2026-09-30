class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.0/zn_0.6.0_darwin_arm64.tar.gz"
      sha256 "5102fce1a2ee2cf9c7358dd0eb8d929023a5675ceb693a96ad2fa50fc6936795"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.0/zn_0.6.0_darwin_amd64.tar.gz"
      sha256 "514a642c820bbbba9f93b6e29626757ba165bc8f69ee967b6304054ad65633b2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.0/zn_0.6.0_linux_arm64.tar.gz"
      sha256 "9c7bfb76ad248585cccd61fedea9a0e6547afd1f14cc96191799996478118142"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.0/zn_0.6.0_linux_amd64.tar.gz"
      sha256 "7e41439f78cceeb80e10a7d0caa68c7ae80986e821cca4d0f5787fdd597c426b"
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
