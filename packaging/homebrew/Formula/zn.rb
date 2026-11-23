class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.2/zn_0.18.2_darwin_arm64.tar.gz"
      sha256 "0905f2bff6bc512aef4a4caa44c56ec6d51e21fe6a434e5c0beeb8f154fcc746"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.2/zn_0.18.2_darwin_amd64.tar.gz"
      sha256 "0982dd2339696e6a59eeccb0b9ba359b510c524ca7671d03725d06b7d65c9085"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.2/zn_0.18.2_linux_arm64.tar.gz"
      sha256 "8a285feddc42da7b6f08de0302b1d94b53682604102cbd03ebe1bf64743f5c15"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.18.2/zn_0.18.2_linux_amd64.tar.gz"
      sha256 "48c9e8f3aecc9244ca983a8524ef265d9c0388fa4946d3e4bc6f733d5946c204"
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
