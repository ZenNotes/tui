class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.1/zn_0.17.1_darwin_arm64.tar.gz"
      sha256 "52f746bc230e14c0c5bd0ad3b714b66b1603a4b4c86848c876df24ef540df9d0"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.1/zn_0.17.1_darwin_amd64.tar.gz"
      sha256 "5e60dc92ba3b58a85df54edc2b9f3fd5d9a11be921659d283ad3c821e22156c9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.1/zn_0.17.1_linux_arm64.tar.gz"
      sha256 "c852436453fd34517f26f93aaaf5033140a6d4bff68e9631aaf2221f14969161"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.1/zn_0.17.1_linux_amd64.tar.gz"
      sha256 "3f7fb5bcb92e6b8969be427ec038aa5cac0a3b814147972d991fab6ef3f0352d"
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
