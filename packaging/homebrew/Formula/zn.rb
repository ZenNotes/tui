class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.3/zn_0.17.3_darwin_arm64.tar.gz"
      sha256 "3bcc766cf99c6ca31dfe7d2ea3d7edc5dbe5b7ab53304a61e1bee952d1e4b976"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.3/zn_0.17.3_darwin_amd64.tar.gz"
      sha256 "093239ad3b4451922b42751a0a8212abd1bfb5a480c2fd3ad7e939dba9fed0e2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.3/zn_0.17.3_linux_arm64.tar.gz"
      sha256 "b8c08678f04de6552e75553a78d09abbd54f27ad2220f7f3871d7858dba4d70b"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.3/zn_0.17.3_linux_amd64.tar.gz"
      sha256 "7ee12a339c2e3cc4b30ac979ea2585c8a3ab1a30cd33f6d9e6f05212622e071f"
    end
  end

  def install
    bin.install "zn"
  end

  def caveats
    <<~EOS
      zn renders display math with typst by default. To draw LaTeX math instead
      (math = "latex" under [terminal] in config.toml), zn runs pdftex and
      pdftocairo installed on your computer; it does not include TeX. On macOS:
        brew install --cask basictex
        brew install poppler
        sudo tlmgr install standalone varwidth preview xcolor amsmath amsfonts mhchem pgf
      On Linux, install your distribution's TeX Live and poppler packages.
      Run zn doctor to check; it prints the commands for your system.
    EOS
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
