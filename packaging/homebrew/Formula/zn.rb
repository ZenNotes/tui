class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_darwin_arm64.tar.gz"
      sha256 "7223eea660a270cf01d2e1015940c131014b8497d0db6b24a0acbb9c546263d7"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_darwin_amd64.tar.gz"
      sha256 "8f3b3190b243d18e84f45a8c4b03eff9c9fa63ea3f815df7a757d674a1f8c652"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_linux_arm64.tar.gz"
      sha256 "f17202e930ac2916cee113d0c7dcacc5bfcbf04db601306a13cc8f757b7c576a"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_linux_amd64.tar.gz"
      sha256 "9b082f58c2db052f44ede345539491f07578745971c847471e5cf1b1786ed256"
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
