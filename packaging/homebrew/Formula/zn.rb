class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.0/zn_0.4.0_darwin_arm64.tar.gz"
      sha256 "1991df8d6893c4bab37ceb0eb34d3aab9026c7d69d265f12212e7ec05af887c3"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.0/zn_0.4.0_darwin_amd64.tar.gz"
      sha256 "69861fbdfedca6710b0b44d9a39ab781ba3073bab6567cca5d92cedfed0c041f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.0/zn_0.4.0_linux_arm64.tar.gz"
      sha256 "3fda655425d9f41b7961e1a8f119bb9282712c7877a31d19b3ac1548a478a2dd"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.4.0/zn_0.4.0_linux_amd64.tar.gz"
      sha256 "03032ff80b14effcecb065533186e72416030b2b69b4c4d3fd67d8cadb73cede"
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
