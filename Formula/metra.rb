class Metra < Formula
  desc "Lightweight terminal system monitor"
  homepage "https://github.com/raywcm/metra"
  version "0.7.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/raywcm/metra/releases/download/v0.7.2/metra_0.7.2_darwin_arm64.tar.gz"
      sha256 "495f8ab74d0a1415abcde540f827e89c9630b8ccc43c393dae8ec39a3a2274bc"
    end
    on_intel do
      url "https://github.com/raywcm/metra/releases/download/v0.7.2/metra_0.7.2_darwin_amd64.tar.gz"
      sha256 "dc3f6eb3b7d13f0b903928b3c3264513cbb7f1b885eee45f31fd9e5b11a084e9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/raywcm/metra/releases/download/v0.7.2/metra_0.7.2_linux_arm64.tar.gz"
      sha256 "d027c676e51dee1f13887ac40342888a2c645cefd4089e07a1fc32542411e24e"
    end
    on_intel do
      url "https://github.com/raywcm/metra/releases/download/v0.7.2/metra_0.7.2_linux_amd64.tar.gz"
      sha256 "9eb35a331ae478bdf06892d97c564936ab690c54966d02a13285d4acf8e8c7af"
    end
  end

  def install
    bin.install "metra"
  end

  test do
    assert_match "metra", shell_output("#{bin}/metra --version")
  end
end
