# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.14'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.14/aviary_v1.0.14_darwin_arm64.tar.gz'
      sha256 '6f121fc2d07b3cb0430e580e22001122291a4e8fd58f4dab85ca755360e45c25'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.14/aviary_v1.0.14_darwin_amd64.tar.gz'
      sha256 'bbcc100798654407ec4821e840b9fee7c8d32d3241b3c2e232274844eb014cf1'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.14/aviary_v1.0.14_linux_arm64.tar.gz'
      sha256 '9fdf31f6b1b31a9b23b1df92e71eb460ce10f1da19282b527bfdc91b6af9a4ca'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.14/aviary_v1.0.14_linux_amd64.tar.gz'
      sha256 '4159af0b30f74de245a508a696842301ff41380388b3105d78df31b42625ceaf'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
