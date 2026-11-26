class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.71"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.71/aviary_v0.7.71_darwin_arm64.tar.gz"
      sha256 "f92ff434c2d95e58bdb1af94d3599cbece13fef2d13b67e9246bb2ba3c185147"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.71/aviary_v0.7.71_darwin_amd64.tar.gz"
      sha256 "e82b4dbc2911e6e5f594efaa6b2b2fa75727457c2bb28f470f2ac54386c9871f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.71/aviary_v0.7.71_linux_arm64.tar.gz"
      sha256 "910eb0f9c19b128994a6c1a7cb0c79d5af274d8d0f1519890921788f238ade1c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.71/aviary_v0.7.71_linux_amd64.tar.gz"
      sha256 "ef16dbce2711c173a7979d2bb06a29927076ebbb2afb2e6edf6901f19f084287"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
