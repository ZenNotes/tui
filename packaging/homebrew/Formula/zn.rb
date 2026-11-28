class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.20.0/zn_0.20.0_darwin_arm64.tar.gz"
      sha256 "43aabae7fc2fafcceb600a07036707f5a23ee3fc0724a6448d2033d6cd437a7c"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.20.0/zn_0.20.0_darwin_amd64.tar.gz"
      sha256 "9c5681b05bf290f51d68947f1c23cdd30c0d4075432e3bd1b8f0f970a1f56b01"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.20.0/zn_0.20.0_linux_arm64.tar.gz"
      sha256 "b86b8220c62e7e6b75b8f0e374e8c3c392f977cfb0b3f361cb7952470612921e"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.20.0/zn_0.20.0_linux_amd64.tar.gz"
      sha256 "083138a798a000c87006d5dbb103484dfb266fa37a3d91e5595e074427a63f68"
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
