class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.54"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.54/aviary_v0.7.54_darwin_arm64.tar.gz"
      sha256 "a236e3e5ae678bad190fcfbe86552426272555691eaef642dfcf7e717386cbe2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.54/aviary_v0.7.54_darwin_amd64.tar.gz"
      sha256 "20e5b173c31c9f6feac824c8a2e802a5d99362a1ef4a10b0821ffe11914737e7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.54/aviary_v0.7.54_linux_arm64.tar.gz"
      sha256 "da1356615cd35f27d77779fbd9f37547184f793a8eee4ae2c302fcc792353314"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.54/aviary_v0.7.54_linux_amd64.tar.gz"
      sha256 "b0579b968f9869f79d2dd2a4690837f5cd82cad8561765195aaa3993b8cae1b4"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
