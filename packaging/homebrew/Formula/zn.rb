class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.0/zn_0.18.0_darwin_arm64.tar.gz"
      sha256 "3b57795dece8e11d7a500ee2b301621d3187145e7264b7242851fd54b473d2a9"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.0/zn_0.18.0_darwin_amd64.tar.gz"
      sha256 "da3bed8f9e436f5c3f3fc78007df704ac18be317890d85e19fc76ba2124b1476"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.0/zn_0.18.0_linux_arm64.tar.gz"
      sha256 "cb4816d57f2a8b2f10748173cf232a04aaf0be40ab2c072fbeeb1e9380a1d48e"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.0/zn_0.18.0_linux_amd64.tar.gz"
      sha256 "7b738d0d5ce98a817d0017689e6fea464649240f42ba4baaae4a5b70de90e538"
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
