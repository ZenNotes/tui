class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.3/zn_0.6.3_darwin_arm64.tar.gz"
      sha256 "531de36cac1f38103c4d05ab80f5aec5e039ae26bdd39e816548541713e1227b"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.3/zn_0.6.3_darwin_amd64.tar.gz"
      sha256 "0a53e218b994ca15be20e6c7004028c1554269013730fedca7611dbd92be154c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.3/zn_0.6.3_linux_arm64.tar.gz"
      sha256 "2735fa644c5769a92080d4f0cb45f0af977fe8c6e96d7e6977272bf877003167"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.3/zn_0.6.3_linux_amd64.tar.gz"
      sha256 "12c033e5d2b9b7fcf407f28b552bcd20d323d2747ea0a2aaea3d0b84d1646198"
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
