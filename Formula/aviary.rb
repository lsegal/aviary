class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.22"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.22/aviary_v0.7.22_darwin_arm64.tar.gz"
      sha256 "b1439cdbcdd9cebd13b724f4340c03955ebc92a980a621b79b6c177ed38a6b69"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.22/aviary_v0.7.22_darwin_amd64.tar.gz"
      sha256 "71d00d4aef208059eef1b9ffec0a07e72af42fb09724c110344e33943cb1310f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.22/aviary_v0.7.22_linux_arm64.tar.gz"
      sha256 "1440d76dca466590214e23e3f0f9b8e48cb5f641ab37db8407cefa77f8eefb01"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.22/aviary_v0.7.22_linux_amd64.tar.gz"
      sha256 "754cf84fd63a5f39995add30292c74b775cec4ba0378d98bcd27cce133ffa89b"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
