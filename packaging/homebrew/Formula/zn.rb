class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.1/zn_0.18.1_darwin_arm64.tar.gz"
      sha256 "fb26893566292629ae8a232289abc9178dd8f0b6a13b0896eda2b77edb12662d"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.1/zn_0.18.1_darwin_amd64.tar.gz"
      sha256 "95242d01306e8915e775bed271a7152639151d7dc05ee5d5a17b58d815cee40d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.1/zn_0.18.1_linux_arm64.tar.gz"
      sha256 "e6798937df01e68bd12a965261fd1a4c4ee7422575889b43329a3fd29a67892b"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.1/zn_0.18.1_linux_amd64.tar.gz"
      sha256 "3b6f87a06abad7b0075bd6c9e304e96ff80387b2c8bef94defc97a240005677a"
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
