class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.3.0/zn_0.3.0_darwin_arm64.tar.gz"
      sha256 "f3b6272fbf74f1415ae6b5b9810e8eb0c9b9330667dc484f484ceef0691c493b"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.3.0/zn_0.3.0_darwin_amd64.tar.gz"
      sha256 "a7a73cc5236bd86070193757a4223e27daa4b3cfbbcc98ed75c617e9ae5a7145"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.3.0/zn_0.3.0_linux_arm64.tar.gz"
      sha256 "08cf083d0ba86b8df2b6d68db135492288bdf653ff58a8c09e4cf81ec491e673"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.3.0/zn_0.3.0_linux_amd64.tar.gz"
      sha256 "cd0e9e2a25e725b5262fcc469f7e5da23c7a02fb60c27d67066d0621f644b4ed"
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
