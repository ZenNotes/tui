class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.15.0/zn_0.15.0_darwin_arm64.tar.gz"
      sha256 "79ac615f9b039f8e2aa9237f286a78dc514649968fa1c6c7acfdb58eb1612730"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.15.0/zn_0.15.0_darwin_amd64.tar.gz"
      sha256 "db3009a4dca21eea15e06771d2b42aaa27721959fafb8b6fad471abb8b50d92d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.15.0/zn_0.15.0_linux_arm64.tar.gz"
      sha256 "39ccbc4a4ef8c23d31cae5edd279af41018bd9e07de6bf8f5430d5cfba897c7d"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.15.0/zn_0.15.0_linux_amd64.tar.gz"
      sha256 "2aae89f413b78050b8f74d369ba37ea5c80efe792bb8bbd6234416405039b07c"
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
