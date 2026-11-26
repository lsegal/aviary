class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.67"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.67/aviary_v0.7.67_darwin_arm64.tar.gz"
      sha256 "75a43332a2ce37f21ab04caa0a77e9f1054c23c48620cceae3de24f8f294c273"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.67/aviary_v0.7.67_darwin_amd64.tar.gz"
      sha256 "369c2bdc2038122e08aab3c9207e6ece9414defc604fa22da8a87481a9f71d4d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.67/aviary_v0.7.67_linux_arm64.tar.gz"
      sha256 "8fb4913ecdf52f2c2aae766b9a75b295b82463d1712709c0cc4a3736694e26fb"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.67/aviary_v0.7.67_linux_amd64.tar.gz"
      sha256 "7610e0d6c27e7f4ae4102b14f09da4bef730ec1e023370ff721fbb916cceb691"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
