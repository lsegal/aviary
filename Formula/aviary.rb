class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.4.5"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.5/aviary_v0.4.5_darwin_arm64.tar.gz"
      sha256 "11c0e2a238e5d7128242c33d73b16f4cc1eb4c81ba9b89ed9e1a8dc53e96f17e"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.5/aviary_v0.4.5_darwin_amd64.tar.gz"
      sha256 "680c87250028231160ef04034fc5c92a0f9cbacd2183138f38cba6b5d0d91709"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.5/aviary_v0.4.5_linux_arm64.tar.gz"
      sha256 "7c2fbee66300aac3056e6536f101d39bd4893a106f8276d28ea7106524fdd215"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.5/aviary_v0.4.5_linux_amd64.tar.gz"
      sha256 "3eac99e4c69e0dbe8b724824fd784ebddea314d4fbf11a9ab58d62e2dcf403c2"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
