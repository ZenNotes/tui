class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.0/zn_0.17.0_darwin_arm64.tar.gz"
      sha256 "5c7952ab0cf074a6ece24cfd28a3961e29c00ed86903d29977678fa03bcc4e80"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.0/zn_0.17.0_darwin_amd64.tar.gz"
      sha256 "33c7e3928638f5a66f58b27e2c978cebbce4422443f2de4a63b85e0d5d78c2c6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.0/zn_0.17.0_linux_arm64.tar.gz"
      sha256 "6c9b7872b753d38e8d8c8db952f6f88e27ad99c03ace93ba14a00225f9f86e5b"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.0/zn_0.17.0_linux_amd64.tar.gz"
      sha256 "2ba1bd4d8729f02927a6379f19120cbf7600c4007a015f3075716c6cc5168f82"
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
