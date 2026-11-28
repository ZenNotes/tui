class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.1/zn_0.19.1_darwin_arm64.tar.gz"
      sha256 "811b7d8919cf02e2c07ee2b2a2c47bc52ecf303828f9496f993f7aee1cb2a21a"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.1/zn_0.19.1_darwin_amd64.tar.gz"
      sha256 "1b11318a3bfb66d06aede520f9279e78ffc7a6fddbde05016af228ffa652c88b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.1/zn_0.19.1_linux_arm64.tar.gz"
      sha256 "4913644808a3923184235b5294734b7e4438cf0f2ed813031ff45218757e6489"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.19.1/zn_0.19.1_linux_amd64.tar.gz"
      sha256 "15d59722aab8976bb983097fe32160090b831b61a112fec5e49ab8457c1f4d9d"
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
