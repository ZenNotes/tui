class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.16.0/zn_0.16.0_darwin_arm64.tar.gz"
      sha256 "054feaf0565b26d4e9f620d88c667504e5a8bfc98e6037bad581651d693b90f5"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.16.0/zn_0.16.0_darwin_amd64.tar.gz"
      sha256 "53029eaadb01a501e4d41c54ea3d367f285dc517e05478f96b9db56c6e8ef745"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.16.0/zn_0.16.0_linux_arm64.tar.gz"
      sha256 "2e6ca65fa14f8e4af0c7ba4b87e460fb06c7a25ed2d70bd73a4b8bce62212ce2"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.16.0/zn_0.16.0_linux_amd64.tar.gz"
      sha256 "8c0aa13c6350fc3e11e602f2aad2759ee556a6b76e1f51f32f4e0a22348d1fcc"
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
