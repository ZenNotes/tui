class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.14.0/zn_0.14.0_darwin_arm64.tar.gz"
      sha256 "69c63952d706c3c19296bef46ca479d4f92bb1cd3ebed1dbd4bc6af77e18d69d"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.14.0/zn_0.14.0_darwin_amd64.tar.gz"
      sha256 "ec2ebb3af08a6df442b8de3a24ced7d1d68b75b8259650a0544385ca59708731"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.14.0/zn_0.14.0_linux_arm64.tar.gz"
      sha256 "bd56c85ec3c7eac2dcdd6f3b8631c86e68f79e9c91e5459c47aa9fd5d022ffa0"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.14.0/zn_0.14.0_linux_amd64.tar.gz"
      sha256 "cb6db04e4dfee155b690ef457a2435a2fc22de2352a34e3c9a870ae14b63b639"
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
