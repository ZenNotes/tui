class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.0/zn_0.19.0_darwin_arm64.tar.gz"
      sha256 "90ccb43817bec52790de972d0bdbf91e172e594a284084578b9c4716a4f1bace"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.0/zn_0.19.0_darwin_amd64.tar.gz"
      sha256 "0504343bf433d1f142da93cf20d944f5017e489253a0b822fb7243863738a9b0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.0/zn_0.19.0_linux_arm64.tar.gz"
      sha256 "4bfc664b128cfe869006ac28adfd2f3d5b5efba3cb89ea5a4a4b9c72fddf7ceb"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.0/zn_0.19.0_linux_amd64.tar.gz"
      sha256 "0ab0ba1c381512a4a2379da6e8bdf78712f0d1ad545e677423a0a540311d2453"
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
